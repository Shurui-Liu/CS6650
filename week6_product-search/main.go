package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Product struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Brand       string `json:"brand"`
}

type SearchResponse struct {
	Products   []Product `json:"products"`
	TotalFound int       `json:"total_found"`
	SearchTime string    `json:"search_time"`
	Checked    int       `json:"checked"` // ← Added
}

var (
	products []Product
	mu       sync.RWMutex // ← Added for thread safety
)

var brands = []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon"}
var categories = []string{"Electronics", "Books", "Home", "Sports", "Clothing"}

func generateProducts() {
	log.Println("Generating 100,000 products...")
	startTime := time.Now()

	for i := 1; i <= 100000; i++ {
		brand := brands[(i-1)%len(brands)]
		category := categories[(i-1)%len(categories)]
		p := Product{
			ID:          i,
			Name:        fmt.Sprintf("Product %s %d", brand, i),
			Category:    category,
			Description: fmt.Sprintf("Description for product %d", i),
			Brand:       brand,
		}
		products = append(products, p)
	}

	elapsed := time.Since(startTime)
	log.Printf("✓ Generated %d products in %v", len(products), elapsed)
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	query := strings.ToLower(r.URL.Query().Get("q"))

	if query == "" {
		http.Error(w, "missing query", http.StatusBadRequest)
		return
	}

	var results []Product
	totalFound := 0
	checked := 0
	const maxCheck = 100
	const maxResults = 20

	mu.RLock() // ← Added lock
	defer mu.RUnlock()

	for _, p := range products {
		if checked >= maxCheck {
			break
		}
		checked++

		if strings.Contains(strings.ToLower(p.Name), query) ||
			strings.Contains(strings.ToLower(p.Category), query) {
			totalFound++
			if len(results) < maxResults {
				results = append(results, p)
			}
		}
	}

	elapsed := time.Since(start)
	log.Printf("Search: query=%s checked=%d found=%d time=%v",
		query, checked, totalFound, elapsed)

	resp := SearchResponse{
		Products:   results,
		TotalFound: totalFound,
		SearchTime: fmt.Sprintf("%.3fms", float64(elapsed.Microseconds())/1000.0),
		Checked:    checked, // ← Added
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json") // ← Added
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{ // ← Changed to JSON
		"status":         "healthy",
		"total_products": len(products),
	})
}

func main() {
	generateProducts()

	http.HandleFunc("/products/search", searchHandler)
	http.HandleFunc("/health", healthHandler)

	log.Println("Server starting on :8080")
	log.Printf("Search endpoint: /products/search?q={query}")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
