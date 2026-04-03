// Package tests contains integration tests for the leaderless KV cluster.
//
// Prerequisites: the leaderless cluster must be running and the leader-follower
// cluster must NOT be running (they share overlapping ports 8081-8084).
//
//	cd leaderless && docker compose up --build -d
//
// Run:
//
//	cd leaderless/tests && go test -v -timeout 120s
//
// Node addresses can be overridden via environment variables:
//
//	NODE_URLS=http://localhost:8081,http://localhost:8082,...
package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
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

func nodeURLs() []string {
	if v := os.Getenv("NODE_URLS"); v != "" {
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
		"http://localhost:8085",
	}
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

type valuePayload struct {
	Value string `json:"value"`
}

// kvPut sends PUT /kv/{key} to nodeURL and returns the HTTP status code.
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

// kvGet sends GET /kv/{key} to nodeURL and returns the value and status code.
// In the leaderless service this always returns the node's own local value (R=1),
// so it is equivalent to local_read — inconsistency is directly observable.
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

// uniqueKey returns a key that is unique per call so tests don't share state.
func uniqueKey(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// pickCoordinator returns a random node index and its URL.
// All other node URLs are returned as peers.
func pickCoordinator(nodes []string) (coordIdx int, coordURL string, peers []string) {
	coordIdx = rand.Intn(len(nodes))
	coordURL = nodes[coordIdx]
	for i, u := range nodes {
		if i != coordIdx {
			peers = append(peers, u)
		}
	}
	return
}

// ---------------------------------------------------------------------------
// Cluster health check — runs before every other test
// ---------------------------------------------------------------------------

// TestClusterHealth verifies that every node responds to a write+read before
// the other tests run. A 405 here usually means the leader-follower cluster
// is still running on overlapping ports; stop it with:
//
//	cd leader-follower && docker compose down
func TestClusterHealth(t *testing.T) {
	nodes := nodeURLs()
	for i, url := range nodes {
		key := uniqueKey(fmt.Sprintf("health-node%d", i+1))
		status, err := kvPut(url, key, "ok")
		if err != nil {
			t.Errorf("node%d (%s): PUT error: %v", i+1, url, err)
			continue
		}
		if status != http.StatusCreated {
			t.Errorf("node%d (%s): PUT returned %d (want 201) — "+
				"is the leader-follower cluster still running on overlapping ports?",
				i+1, url, status)
			continue
		}
		got, status, err := kvGet(url, key)
		if err != nil || status != http.StatusOK || got != "ok" {
			t.Errorf("node%d (%s): GET returned status=%d value=%q err=%v",
				i+1, url, status, got, err)
		} else {
			t.Logf("node%d (%s): healthy", i+1, url)
		}
	}
}

// ---------------------------------------------------------------------------
// writeResult carries the outcome of an async write for validation.
// ---------------------------------------------------------------------------

type writeResult struct {
	status int
	err    error
}

func (r writeResult) ok() bool { return r.err == nil && r.status == http.StatusCreated }

// ---------------------------------------------------------------------------
// Test 1 — Inconsistency window: reads to non-coordinator nodes during a write
// ---------------------------------------------------------------------------
//
// The coordinator replicates to 4 peers sequentially, sleeping 200 ms after
// each send, with each peer sleeping 100 ms before acking. The last peer is
// not updated for ~1200 ms after the write starts.
//
// Strategy:
//  1. Fire the write to a randomly chosen coordinator in a goroutine.
//  2. After 50 ms — in-flight but far from complete — read from every other node.
//  3. Nodes that haven't been reached yet return 404, proving the window exists.
//
// The test does NOT fail on inconsistency; it logs and counts them.

func TestInconsistencyWindow(t *testing.T) {
	const iterations = 10

	nodes := nodeURLs()
	var totalChecks, inconsistencies int64

	for i := 0; i < iterations; i++ {
		coordIdx, coordURL, peers := pickCoordinator(nodes)
		key := uniqueKey(fmt.Sprintf("window-%d", i))
		want := fmt.Sprintf("v%d", i)

		t.Logf("iter %2d  coordinator=node%d (%s)", i+1, coordIdx+1, coordURL)

		// Fire the write; do NOT wait for 201.
		ready := make(chan struct{})
		done := make(chan writeResult, 1)
		go func(url, k, v string) {
			close(ready) // signal: goroutine is running and about to write
			status, err := kvPut(url, k, v)
			done <- writeResult{status, err}
		}(coordURL, key, want)

		// Wait until the goroutine is running, then give the HTTP request a
		// small head-start so it is genuinely in-flight.
		<-ready
		time.Sleep(50 * time.Millisecond)

		// Concurrently read from all non-coordinator nodes.
		type readResult struct {
			nodeIdx int
			value   string
			code    int
			err     error
		}
		ch := make(chan readResult, len(peers))
		for pi, pURL := range peers {
			go func(pi int, pURL string) {
				v, code, err := kvGet(pURL, key)
				ch <- readResult{pi, v, code, err}
			}(pi, pURL)
		}

		// Collect peer read results.
		var peerResults []readResult
		for range peers {
			peerResults = append(peerResults, <-ch)
		}

		// Block until the write finishes so we know it was valid.
		wr := <-done
		if !wr.ok() {
			t.Errorf("iter %d: write FAILED (status=%d err=%v) — skipping reads "+
				"(check cluster health; leader-follower cluster may still be running)",
				i+1, wr.status, wr.err)
			continue
		}

		// Now evaluate peer reads against the write that we know succeeded.
		for _, res := range peerResults {
			atomic.AddInt64(&totalChecks, 1)
			consistent := res.err == nil && res.code == http.StatusOK && res.value == want
			if !consistent {
				atomic.AddInt64(&inconsistencies, 1)
				t.Logf("         peer[%d]  INCONSISTENT  code=%d  got=%q  want=%q  err=%v",
					res.nodeIdx, res.code, res.value, want, res.err)
			} else {
				t.Logf("         peer[%d]  consistent    got=%q", res.nodeIdx, res.value)
			}
		}
	}

	pct := float64(inconsistencies) / float64(totalChecks) * 100
	t.Logf("\nInconsistency window summary: %d/%d reads inconsistent (%.1f%%)",
		inconsistencies, totalChecks, pct)
	if inconsistencies == 0 {
		t.Log("WARNING: no inconsistencies observed — try reducing the 50 ms probe delay")
	}
}

// ---------------------------------------------------------------------------
// Test 2 — Coordinator read after 201 must be consistent
// ---------------------------------------------------------------------------
//
// Once the coordinator returns 201 it has already written locally (that write
// happens before any replication), so reading back from it must always succeed.

func TestCoordinatorReadAfterWrite(t *testing.T) {
	nodes := nodeURLs()
	coordIdx, coordURL, _ := pickCoordinator(nodes)
	key := uniqueKey("coord-read")
	want := "coordinator-value"

	t.Logf("coordinator=node%d (%s)", coordIdx+1, coordURL)

	status, err := kvPut(coordURL, key, want)
	if err != nil {
		t.Fatalf("PUT error: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("PUT returned %d (want 201) — is the leader-follower cluster still running?", status)
	}

	got, status, err := kvGet(coordURL, key)
	if err != nil {
		t.Fatalf("GET from coordinator failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("GET returned %d (want 200)", status)
	}
	if got != want {
		t.Errorf("coordinator read: got %q, want %q", got, want)
	} else {
		t.Logf("coordinator read: consistent (%q)", got)
	}
}

// ---------------------------------------------------------------------------
// Test 3 — Peer reads after 201 must be consistent (W=N guarantee)
// ---------------------------------------------------------------------------
//
// With W=N the coordinator only returns 201 after every peer has acked.
// Therefore a read from any node immediately after 201 must return the
// correct value — there is no inconsistency window post-ack.

func TestPeerReadConsistencyAfterWrite(t *testing.T) {
	nodes := nodeURLs()
	coordIdx, coordURL, peers := pickCoordinator(nodes)
	key := uniqueKey("peer-read")
	want := "fully-replicated-value"

	t.Logf("coordinator=node%d (%s)", coordIdx+1, coordURL)

	status, err := kvPut(coordURL, key, want)
	if err != nil {
		t.Fatalf("PUT error: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("PUT returned %d (want 201) — is the leader-follower cluster still running?", status)
	}

	for pi, pURL := range peers {
		t.Run(fmt.Sprintf("peer%d", pi+1), func(t *testing.T) {
			got, status, err := kvGet(pURL, key)
			if err != nil {
				t.Fatalf("GET failed: %v", err)
			}
			if status != http.StatusOK {
				t.Errorf("GET returned %d (want 200)", status)
				return
			}
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			} else {
				t.Logf("peer%d: consistent (%q)", pi+1, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 4 — High-load sweep: many concurrent writes expose the window reliably
// ---------------------------------------------------------------------------

func TestHighLoadInconsistency(t *testing.T) {
	const concurrentWrites = 20

	nodes := nodeURLs()
	type job struct {
		coordURL string
		key      string
		value    string
	}

	jobs := make([]job, concurrentWrites)
	for i := range jobs {
		_, coordURL, _ := pickCoordinator(nodes)
		jobs[i] = job{
			coordURL: coordURL,
			key:      uniqueKey(fmt.Sprintf("load-%d", i)),
			value:    fmt.Sprintf("val-%d", i),
		}
	}

	// Fire all writes concurrently; collect their results.
	results := make(chan writeResult, concurrentWrites)
	for _, j := range jobs {
		go func(j job) {
			status, err := kvPut(j.coordURL, j.key, j.value)
			results <- writeResult{status, err}
		}(j)
	}

	// Let writes get into the replication pipeline before probing.
	time.Sleep(80 * time.Millisecond)

	// For each write, read from every node that is NOT the coordinator.
	var totalChecks, inconsistencies int64
	for _, j := range jobs {
		for _, nURL := range nodes {
			if nURL == j.coordURL {
				continue
			}
			v, code, err := kvGet(nURL, j.key)
			atomic.AddInt64(&totalChecks, 1)
			if err != nil || code != http.StatusOK || v != j.value {
				atomic.AddInt64(&inconsistencies, 1)
				t.Logf("key=%-35s  node=%-28s  INCONSISTENT  code=%d  got=%q",
					j.key, nURL, code, v)
			}
		}
	}

	// Drain write results and fail if any write itself failed.
	for i := 0; i < concurrentWrites; i++ {
		wr := <-results
		if !wr.ok() {
			t.Errorf("write %d failed: status=%d err=%v", i, wr.status, wr.err)
		}
	}

	pct := float64(inconsistencies) / float64(totalChecks) * 100
	t.Logf("\nHigh-load summary: %d/%d reads inconsistent (%.1f%%)",
		inconsistencies, totalChecks, pct)
}
