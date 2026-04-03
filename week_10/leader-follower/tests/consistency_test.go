// Package tests contains integration tests for the leader-follower KV cluster.
//
// Prerequisites: the cluster must be running before executing these tests.
//
//	cd leader-follower && docker compose up --build -d
//
// Run:
//
//	cd leader-follower/tests && go test -v -timeout 120s
//
// Node addresses can be overridden via environment variables:
//
//	LEADER_URL=http://localhost:8080
//	FOLLOWER_URLS=http://localhost:8081,http://localhost:8082,http://localhost:8083,http://localhost:8084
package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

func leaderURL() string {
	if v := os.Getenv("LEADER_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func followerURLs() []string {
	if v := os.Getenv("FOLLOWER_URLS"); v != "" {
		var urls []string
		for _, u := range strings.Split(v, ",") {
			if u = strings.TrimSpace(u); u != "" {
				urls = append(urls, u)
			}
		}
		return urls
	}
	return []string{
		"http://localhost:8081",
		"http://localhost:8082",
		"http://localhost:8083",
		"http://localhost:8084",
	}
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

type valuePayload struct {
	Value string `json:"value"`
}

// kvPut sends PUT /kv/{key} with the given value and returns the status code.
func kvPut(nodeURL, key, value string) (int, error) {
	body, _ := json.Marshal(valuePayload{Value: value})
	req, err := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/kv/%s", nodeURL, key),
		bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// kvGet sends GET /kv/{key} and returns the value and status code.
func kvGet(nodeURL, key string) (string, int, error) {
	resp, err := http.Get(fmt.Sprintf("%s/kv/%s", nodeURL, key))
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode, nil
	}
	var p valuePayload
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return "", resp.StatusCode, err
	}
	return p.Value, resp.StatusCode, nil
}

// localRead sends GET /local_read/{key} — the sneaky testing-only endpoint
// that returns whatever the node holds locally right now, no quorum or delays.
func localRead(nodeURL, key string) (string, int, error) {
	resp, err := http.Get(fmt.Sprintf("%s/local_read/%s", nodeURL, key))
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode, nil
	}
	var p valuePayload
	if err := json.Unmarshal(body, &p); err != nil {
		return "", resp.StatusCode, err
	}
	return p.Value, resp.StatusCode, nil
}

// uniqueKey generates a unique key per test run to avoid cross-test pollution.
func uniqueKey(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// ---------------------------------------------------------------------------
// Test 1 — Read from the Leader after a write must be consistent
// ---------------------------------------------------------------------------

func TestLeaderReadConsistency(t *testing.T) {
	key := uniqueKey("leader-read")
	want := "hello-from-leader"

	// Write to leader.
	status, err := kvPut(leaderURL(), key, want)
	if err != nil {
		t.Fatalf("PUT failed: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("PUT: expected 201, got %d", status)
	}

	// Read back from the same leader node.
	got, status, err := kvGet(leaderURL(), key)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d", status)
	}
	if got != want {
		t.Errorf("leader read: got %q, want %q", got, want)
	} else {
		t.Logf("leader read: consistent (%q)", got)
	}
}

// ---------------------------------------------------------------------------
// Test 2 — Read from every Follower after a write must be consistent (W=5)
// ---------------------------------------------------------------------------
//
// With W=5 the leader waits for every follower to ack before returning 201,
// so by the time the client sees the response all nodes are up to date.

func TestFollowerReadConsistency(t *testing.T) {
	key := uniqueKey("follower-read")
	want := "propagated-value"

	status, err := kvPut(leaderURL(), key, want)
	if err != nil {
		t.Fatalf("PUT failed: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("PUT: expected 201, got %d", status)
	}

	for i, fURL := range followerURLs() {
		t.Run(fmt.Sprintf("follower%d", i+1), func(t *testing.T) {
			got, status, err := kvGet(fURL, key)
			if err != nil {
				t.Fatalf("GET failed: %v", err)
			}
			if status != http.StatusOK {
				t.Errorf("GET: expected 200, got %d", status)
				return
			}
			if got != want {
				t.Errorf("follower%d: got %q, want %q", i+1, got, want)
			} else {
				t.Logf("follower%d: consistent (%q)", i+1, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 3 — Catch the inconsistency window with local_read
// ---------------------------------------------------------------------------
//
// The leader replicates to followers sequentially, sleeping 200 ms after each
// send, and each follower sleeps 100 ms before acking.  That means the last
// follower is not updated until ~1 200 ms after the write starts.
//
// Strategy:
//  1. Fire the write to the leader in a goroutine (do not wait for 201).
//  2. After 50 ms — enough time for the write to be in-flight but not complete —
//     poll every follower via /local_read.
//  3. Followers that have not been reached yet will return 404 or the old value.
//
// The test does NOT fail on inconsistency; it logs and counts them.
// Run with -count=5 or increase `iterations` to raise the hit rate.

func TestInconsistencyWindowLocalRead(t *testing.T) {
	const iterations = 10

	followers := followerURLs()
	var totalChecks, inconsistencies int64

	for i := 0; i < iterations; i++ {
		key := uniqueKey(fmt.Sprintf("window-%d", i))
		want := fmt.Sprintf("v%d", i)

		// Channel closed the moment the goroutine is scheduled and about to write.
		ready := make(chan struct{})
		writeDone := make(chan error, 1)

		go func(k, v string) {
			close(ready)
			_, err := kvPut(leaderURL(), k, v)
			writeDone <- err
		}(key, want)

		// Wait until the goroutine is running, then give the HTTP request a
		// tiny head-start so it is genuinely in-flight when we start polling.
		<-ready
		time.Sleep(50 * time.Millisecond)

		// Poll all followers simultaneously via local_read.
		type result struct {
			idx   int
			value string
			code  int
			err   error
		}
		ch := make(chan result, len(followers))
		for idx, fURL := range followers {
			go func(idx int, fURL string) {
				v, code, err := localRead(fURL, key)
				ch <- result{idx, v, code, err}
			}(idx, fURL)
		}

		for range followers {
			res := <-ch
			atomic.AddInt64(&totalChecks, 1)
			consistent := res.err == nil && res.code == http.StatusOK && res.value == want
			if !consistent {
				atomic.AddInt64(&inconsistencies, 1)
				t.Logf("iter %2d  follower%d  INCONSISTENT  code=%d value=%q want=%q err=%v",
					i+1, res.idx+1, res.code, res.value, want, res.err)
			} else {
				t.Logf("iter %2d  follower%d  consistent    value=%q", i+1, res.idx+1, res.value)
			}
		}

		// Wait for the write to finish before the next iteration so keys don't collide.
		if err := <-writeDone; err != nil {
			t.Errorf("iter %d: write error: %v", i+1, err)
		}
	}

	pct := float64(inconsistencies) / float64(totalChecks) * 100
	t.Logf("\nSummary: %d/%d reads inconsistent (%.1f%%)", inconsistencies, totalChecks, pct)

	if inconsistencies == 0 {
		t.Log("WARNING: no inconsistencies observed — the 50 ms probe delay may have been too long; " +
			"try reducing it or running under higher load")
	}
}

// ---------------------------------------------------------------------------
// Test 4 — High-load inconsistency sweep
// ---------------------------------------------------------------------------
//
// Fires many concurrent writes and immediately local_reads every follower.
// At high concurrency the replication pipeline is always busy and the
// inconsistency window is reliably visible.

func TestHighLoadInconsistency(t *testing.T) {
	const concurrentWrites = 20

	followers := followerURLs()
	var totalChecks, inconsistencies int64

	type writeJob struct {
		key, value string
	}
	jobs := make([]writeJob, concurrentWrites)
	for i := range jobs {
		jobs[i] = writeJob{
			key:   uniqueKey(fmt.Sprintf("load-%d", i)),
			value: fmt.Sprintf("val-%d", i),
		}
	}

	// Start all writes concurrently.
	writeDone := make(chan error, concurrentWrites)
	for _, job := range jobs {
		go func(j writeJob) {
			_, err := kvPut(leaderURL(), j.key, j.value)
			writeDone <- err
		}(job)
	}

	// Give writes a moment to get into the replication pipeline.
	time.Sleep(80 * time.Millisecond)

	// Probe every follower for every key right now.
	for _, job := range jobs {
		for idx, fURL := range followers {
			v, code, err := localRead(fURL, job.key)
			atomic.AddInt64(&totalChecks, 1)
			if err != nil || code != http.StatusOK || v != job.value {
				atomic.AddInt64(&inconsistencies, 1)
				t.Logf("key=%-30s  follower%d  INCONSISTENT  code=%d got=%q",
					job.key, idx+1, code, v)
			}
		}
	}

	// Drain writes.
	for i := 0; i < concurrentWrites; i++ {
		if err := <-writeDone; err != nil {
			t.Errorf("write error: %v", err)
		}
	}

	pct := float64(inconsistencies) / float64(totalChecks) * 100
	t.Logf("\nSummary: %d/%d reads inconsistent (%.1f%%)", inconsistencies, totalChecks, pct)
}
