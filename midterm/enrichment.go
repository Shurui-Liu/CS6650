package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
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

// enrichProduct calls the enrichment (chaos) service with NO timeout and
// NO circuit breaker. This is intentionally broken for Part 1 of the demo.
//
// When the chaos service is set to "slow", this call will block for 20 seconds,
// holding a goroutine open for the entire duration.
func enrichProduct(p Product) Product {
	url := fmt.Sprintf("%s/enrich/%d", enrichmentURL(), p.ID)

	// ⚠️  No timeout set on this client — uses Go's default (forever)
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		log.Printf("enrichment failed for product %d: %v", p.ID, err)
		return p // silently return unenriched product
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("enrichment non-200 for product %d: %d", p.ID, resp.StatusCode)
		return p
	}

	var er enrichmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return p
	}

	// Tack enrichment data onto the description so it shows up in responses
	p.Description = fmt.Sprintf("%s | Price: %s | Availability: %s",
		p.Description, er.Price, er.Availability)
	return p
}
