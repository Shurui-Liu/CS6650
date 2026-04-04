// loadtest is a configurable load-test client for the leader-follower and
// leaderless KV databases.
//
// Key design decisions:
//
//   - "Local-in-time" clustering is achieved by using a small key space
//     (default 100 keys). With many workers and few keys, reads and writes
//     to the same key cluster tightly in time, making stale reads observable.
//
//   - Stale-read detection: the client tracks the last successfully written
//     value per key. Before issuing a GET the client snapshots the expected
//     value; if the response differs, the read is marked stale.
//
//   - Values are unix-nanosecond timestamps encoded as strings, which means
//     "newer" values are lexicographically greater and trivially human-readable.
//
// Output: newline-delimited JSON (one Record per line) written to -output.
//
// Usage examples:
//
//	# Leader-Follower W=5 R=1, 10% writes
//	./loadtest -label W5R1 \
//	  -write-urls http://localhost:8080 \
//	  -read-urls  http://localhost:8081,http://localhost:8082,http://localhost:8083,http://localhost:8084 \
//	  -write-ratio 0.10 -duration 30s -output W5R1_10pct.jsonl
//
//	# Leaderless, 50% writes (writes and reads go to any node)
//	./loadtest -label leaderless \
//	  -write-urls http://localhost:8081,http://localhost:8082,http://localhost:8083,http://localhost:8084,http://localhost:8085 \
//	  -read-urls  http://localhost:8081,http://localhost:8082,http://localhost:8083,http://localhost:8084,http://localhost:8085 \
//	  -write-ratio 0.50 -duration 30s -output leaderless_50pct.jsonl
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Record captures the outcome of a single request.
type Record struct {
	// When the request was issued (ms since Unix epoch, float for sub-ms precision).
	TimestampMs float64 `json:"ts_ms"`
	// "read" or "write"
	Type string `json:"type"`
	// Key that was read or written.
	Key string `json:"key"`
	// End-to-end round-trip time in milliseconds.
	LatencyMs float64 `json:"latency_ms"`
	// HTTP status code returned by the server.
	StatusCode int `json:"status_code"`
	// Reads only: true if the returned value differs from the last confirmed write.
	IsStale bool `json:"is_stale"`
	// Reads only: milliseconds between the last confirmed write to this key
	// and the start of this read request. -1 if this key has never been written.
	RWGapMs float64 `json:"rw_gap_ms"`
	// Run label, e.g. "W5R1", "W1R5", "W3R3", "leaderless".
	Label string `json:"label"`
	// Fraction of operations that were writes (0.01, 0.10, 0.50, 0.90).
	WriteRatio float64 `json:"write_ratio"`
}

// keyState holds the last confirmed write to a single key.
// Protected by a per-key mutex so concurrent goroutines don't race.
type keyState struct {
	mu            sync.Mutex
	lastValue     string    // value from the most recent successful PUT
	hasValue      bool      // false until the first write completes
	lastWriteTime time.Time // wall-clock time when that write returned 201
}

func main() {
	writeURLsFlag := flag.String("write-urls", "http://localhost:8080",
		"comma-separated endpoint(s) that accept writes (leader, or any node for leaderless)")
	readURLsFlag := flag.String("read-urls",
		"http://localhost:8081,http://localhost:8082,http://localhost:8083,http://localhost:8084",
		"comma-separated endpoint(s) that serve reads (followers, or any node for leaderless)")
	writeRatio := flag.Float64("write-ratio", 0.5,
		"fraction of operations that are writes: 0.01 / 0.10 / 0.50 / 0.90")
	keyCount := flag.Int("keys", 100,
		"size of the key space — smaller means tighter read/write clustering per key")
	duration := flag.Duration("duration", 30*time.Second,
		"how long to run the load test")
	concurrency := flag.Int("concurrency", 10,
		"number of parallel workers")
	outputFile := flag.String("output", "results.jsonl",
		"output file (newline-delimited JSON)")
	label := flag.String("label", "test",
		`run label written into every record, e.g. "W5R1", "W1R5", "W3R3", "leaderless"`)
	flag.Parse()

	wURLs := splitTrim(*writeURLsFlag)
	rURLs := splitTrim(*readURLsFlag)
	if len(wURLs) == 0 {
		log.Fatal("-write-urls cannot be empty")
	}
	if len(rURLs) == 0 {
		log.Fatal("-read-urls cannot be empty")
	}

	log.Printf("label=%-12s write_ratio=%4.0f%%  keys=%d  duration=%v  concurrency=%d",
		*label, *writeRatio*100, *keyCount, *duration, *concurrency)
	log.Printf("write endpoints: %v", wURLs)
	log.Printf("read  endpoints: %v", rURLs)

	// One state tracker per key — shared across all workers.
	states := make([]*keyState, *keyCount)
	for i := range states {
		states[i] = &keyState{}
	}

	var (
		mu      sync.Mutex
		records []Record
		total   int64
	)

	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(*duration)
	var wg sync.WaitGroup

	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Each worker gets its own RNG seeded uniquely.
			rng := rand.New(rand.NewSource(time.Now().UnixNano() ^ int64(id*1_000_003)))

			for time.Now().Before(deadline) {
				keyIdx := rng.Intn(*keyCount)
				key := fmt.Sprintf("key%04d", keyIdx)

				var rec Record
				if rng.Float64() < *writeRatio {
					rec = doWrite(client, wURLs, rng, key, states[keyIdx], *label, *writeRatio)
				} else {
					rec = doRead(client, rURLs, rng, key, states[keyIdx], *label, *writeRatio)
				}

				n := atomic.AddInt64(&total, 1)
				if n%5_000 == 0 {
					log.Printf("  %d requests", n)
				}

				mu.Lock()
				records = append(records, rec)
				mu.Unlock()
			}
		}(w)
	}

	wg.Wait()

	// Write output file.
	f, err := os.Create(*outputFile)
	if err != nil {
		log.Fatalf("create %s: %v", *outputFile, err)
	}
	enc := json.NewEncoder(f)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			log.Printf("encode record: %v", err)
		}
	}
	f.Close()

	// Print summary.
	var nWrites, nReads, nStale int
	for _, r := range records {
		switch r.Type {
		case "write":
			nWrites++
		case "read":
			nReads++
			if r.IsStale {
				nStale++
			}
		}
	}
	stalePC := 0.0
	if nReads > 0 {
		stalePC = float64(nStale) / float64(nReads) * 100
	}
	log.Printf("done — writes=%d  reads=%d  stale=%d (%.2f%%)",
		nWrites, nReads, nStale, stalePC)
	log.Printf("results → %s", *outputFile)
}

// doWrite issues PUT /kv/{key} and records the outcome.
// On success (201) it updates the per-key state so subsequent reads can
// detect staleness.
func doWrite(client *http.Client, urls []string, rng *rand.Rand,
	key string, state *keyState, label string, writeRatio float64) Record {

	// Use the current nanosecond timestamp as the value.
	// This is monotonically increasing, human-readable, and unique enough
	// for stale-read detection.
	value := fmt.Sprintf("%d", time.Now().UnixNano())

	url := fmt.Sprintf("%s/kv/%s", urls[rng.Intn(len(urls))], key)
	body, _ := json.Marshal(map[string]string{"value": value})
	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	rec := Record{
		TimestampMs: float64(start.UnixNano()) / 1e6,
		Type:        "write",
		Key:         key,
		LatencyMs:   float64(latency.Microseconds()) / 1000.0,
		RWGapMs:     -1,
		Label:       label,
		WriteRatio:  writeRatio,
	}
	if err != nil {
		return rec
	}
	resp.Body.Close()
	rec.StatusCode = resp.StatusCode

	if resp.StatusCode == http.StatusCreated {
		state.mu.Lock()
		state.lastValue = value
		state.hasValue = true
		state.lastWriteTime = time.Now()
		state.mu.Unlock()
	}
	return rec
}

// doRead issues GET /kv/{key} and records the outcome.
// It snapshots the expected value BEFORE the request so that any
// replication lag visible in the response is correctly labelled stale.
func doRead(client *http.Client, urls []string, rng *rand.Rand,
	key string, state *keyState, label string, writeRatio float64) Record {

	// Snapshot expected state before issuing the request.
	state.mu.Lock()
	expectedValue := state.lastValue
	hasExpected := state.hasValue
	lastWriteTime := state.lastWriteTime
	state.mu.Unlock()

	url := fmt.Sprintf("%s/kv/%s", urls[rng.Intn(len(urls))], key)

	start := time.Now()
	resp, err := client.Get(url)
	latency := time.Since(start)

	rec := Record{
		TimestampMs: float64(start.UnixNano()) / 1e6,
		Type:        "read",
		Key:         key,
		LatencyMs:   float64(latency.Microseconds()) / 1000.0,
		RWGapMs:     -1,
		Label:       label,
		WriteRatio:  writeRatio,
	}
	if err != nil {
		return rec
	}
	defer resp.Body.Close()
	rec.StatusCode = resp.StatusCode

	if resp.StatusCode == http.StatusOK && hasExpected {
		var p struct {
			Value string `json:"value"`
		}
		if json.NewDecoder(resp.Body).Decode(&p) == nil {
			rec.IsStale = p.Value != expectedValue
			rec.RWGapMs = float64(start.Sub(lastWriteTime).Microseconds()) / 1000.0
		}
	}
	return rec
}

func splitTrim(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
