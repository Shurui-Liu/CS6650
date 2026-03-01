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

// ── Data model ────────────────────────────────────────────────────────────────

type Product struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Brand       string `json:"brand"`
}

type SearchResponse struct {
	Products        []Product `json:"products"`
	TotalFound      int       `json:"total_found"`
	SearchTime      string    `json:"search_time"`
	ProductsChecked int       `json:"products_checked"`
}

// ── In-memory store ───────────────────────────────────────────────────────────

var store sync.Map

var (
	brands     = []string{"Alpha", "Beta", "Gamma", "Delta", "Omega"}
	categories = []string{"Electronics", "Books", "Home", "Sports", "Clothing", "Toys", "Garden", "Automotive"}
)

func generateProducts() {
	for i := 1; i <= 100_000; i++ {
		brand := brands[i%len(brands)]
		cat := categories[i%len(categories)]
		p := Product{
			ID:          i,
			Name:        fmt.Sprintf("Product %s %d", brand, i),
			Category:    cat,
			Description: fmt.Sprintf("A quality %s product from %s, item number %d.", cat, brand, i),
			Brand:       brand,
		}
		store.Store(i, p)
	}
	log.Println("✅ Generated 100,000 products")
}

// ── Search logic ──────────────────────────────────────────────────────────────

func searchProducts(query string) SearchResponse {
	start := time.Now()
	q := strings.ToLower(query)

	var results []Product
	checked := 0

	store.Range(func(_, val any) bool {
		checked++
		if checked > 100 { // check exactly 100 products then stop
			return false
		}
		p := val.(Product)
		if strings.Contains(strings.ToLower(p.Name), q) ||
			strings.Contains(strings.ToLower(p.Category), q) {
			results = append(results, p)
		}
		return true
	})

	total := len(results)
	if len(results) > 20 {
		results = results[:20]
	}

	return SearchResponse{
		Products:        results,
		TotalFound:      total,
		SearchTime:      time.Since(start).String(),
		ProductsChecked: checked,
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func searchHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, `{"error":"missing query param q"}`, http.StatusBadRequest)
		return
	}

	resp := searchProducts(q)

	// 💣 BROKEN: enrich each result with NO timeout, NO circuit breaker.
	// One slow dependency call blocks this entire response.
	for i, p := range resp.Products {
		resp.Products[i] = enrichProduct(p)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	generateProducts()

	http.HandleFunc("/products/search", searchHandler)
	http.HandleFunc("/health", healthHandler)

	log.Println("🚀 Search service listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
