package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/sony/gobreaker"
)

// enrichmentURL reads the chaos service address from an env var so it works
// both locally (docker-compose) and on ECS without code changes.
func enrichmentURL() string {
	host := os.Getenv("ENRICHMENT_HOST")
	if host == "" {
		host = "http://dependent-service:9090"
	}
	return host
}

type enrichmentResponse struct {
	Price        string `json:"price"`
	Availability string `json:"availability"`
}

// ── Fix 1: Fail Fast ──────────────────────────────────────────────────────────
// Use a client with a strict 200ms timeout.
// Worst case degradation is now 200ms, not 20 seconds.
var httpClient = &http.Client{
	Timeout: 200 * time.Millisecond,
}

// ── Fix 2: Circuit Breaker ────────────────────────────────────────────────────
// Wraps the enrichment call. Once too many failures are detected,
// the circuit opens and requests return instantly without hitting the network.
var enrichBreaker = gobreaker.NewCircuitBreaker(gobreaker.Settings{
	Name:        "enrichment-service",
	MaxRequests: 3,                // half-open: allow 3 test requests through
	Interval:    10 * time.Second, // reset failure count every 10s
	Timeout:     30 * time.Second, // stay OPEN for 30s before trying again

	ReadyToTrip: func(counts gobreaker.Counts) bool {
		// Open the circuit if more than 50% of the last 10 requests failed
		return counts.Requests >= 10 && counts.TotalFailures > counts.Requests/2
	},

	OnStateChange: func(name string, from, to gobreaker.State) {
		// This logs every state transition — great for your demo!
		// In production you would emit this as a CloudWatch custom metric.
		log.Printf("🔴 Circuit breaker [%s]: %s → %s", name, from, to)
	},
})

// enrichProduct calls the dependent service with:
//   - A 200ms timeout (Fail Fast)
//   - A circuit breaker (stops calling if too many failures)
//
// If either protection kicks in, the original unenriched product is returned.
func enrichProduct(p Product) Product {
	result, err := enrichBreaker.Execute(func() (interface{}, error) {
		url := fmt.Sprintf("%s/enrich/%d", enrichmentURL(), p.ID)

		resp, err := httpClient.Get(url) //nolint:gosec
		if err != nil {
			return p, err // timeout or connection error — circuit counts this as a failure
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return p, fmt.Errorf("enrichment returned status %d", resp.StatusCode)
		}

		var er enrichmentResponse
		if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
			return p, err
		}

		p.Description = fmt.Sprintf("%s | Price: %s | Availability: %s",
			p.Description, er.Price, er.Availability)
		return p, nil
	})

	if err != nil {
		// Circuit is OPEN or request failed — return unenriched product instantly
		return p
	}

	return result.(Product)
}

// CircuitState returns the current state of the breaker as a string.
// Used by the /health endpoint in main.go.
func CircuitState() string {
	return enrichBreaker.State().String() // "closed", "open", or "half-open"
}
