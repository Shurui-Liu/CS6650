package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// mode controls the chaos service behaviour and can be toggled live via HTTP.
// Start it in "healthy" mode — flip it during the demo for maximum drama.
var (
	mu   sync.RWMutex
	mode = "healthy"
)

func getMode() string {
	mu.RLock()
	defer mu.RUnlock()
	return mode
}

func setMode(m string) {
	mu.Lock()
	defer mu.Unlock()
	mode = m
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// enrichHandler simulates a downstream pricing/inventory service.
func enrichHandler(w http.ResponseWriter, r *http.Request) {
	m := getMode()
	log.Printf("[chaos] /enrich called — mode=%s", m)

	switch m {
	case "slow":
		// Sleeps 20 seconds — this is the bomb that breaks the search service
		log.Println("[chaos] 💣 sleeping 20s (slow mode)")
		time.Sleep(20 * time.Second)

	case "dead":
		// Returns 503 immediately — useful for testing circuit breaker
		log.Println("[] ☠️  returning 503 (dead mode)")
		http.Error(w, `{"error":"service unavailable"}`, http.StatusServiceUnavailable)
		return

	default: // "healthy"
		// Fast response — baseline behaviour
		time.Sleep(5 * time.Millisecond)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"price":        "$99.99",
		"availability": "in stock",
	})
}

// modeHandler lets you toggle chaos live without redeploying.
//
//	GET  /mode          → returns current mode
//	POST /mode?m=slow   → set to slow
//	POST /mode?m=dead   → set to dead
//	POST /mode?m=healthy → set back to healthy
func modeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		m := r.URL.Query().Get("m")
		if m != "healthy" && m != "slow" && m != "dead" {
			http.Error(w, `{"error":"valid modes: healthy, slow, dead"}`, http.StatusBadRequest)
			return
		}
		setMode(m)
		log.Printf("[chaos] ⚙️  mode changed to: %s", m)
		fmt.Fprintf(w, `{"mode":"%s"}`, m)
		return
	}
	// GET — return current mode
	fmt.Fprintf(w, `{"mode":"%s"}`, getMode())
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `{"status":"ok","mode":"%s"}`, getMode())
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	http.HandleFunc("/enrich/", enrichHandler)
	http.HandleFunc("/mode", modeHandler)
	http.HandleFunc("/health", healthHandler)

	log.Println("🎭 Chaos service listening on :9090  (default mode: healthy)")
	log.Println("   Flip modes with:")
	log.Println("     curl -X POST 'http://localhost:9090/mode?m=slow'")
	log.Println("     curl -X POST 'http://localhost:9090/mode?m=dead'")
	log.Println("     curl -X POST 'http://localhost:9090/mode?m=healthy'")
	log.Fatal(http.ListenAndServe(":9090", nil))
}
