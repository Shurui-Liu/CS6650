package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// KEEP YOUR EXISTING PRODUCT STRUCTURE
// (or update if needed for search requirements)
type Product struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	Description  string `json:"description"`
	Brand        string `json:"brand"`
	SKU          string `json:"sku"`
	Manufacturer string `json:"manufacturer"`
	CategoryID   int    `json:"category_id"`
	Weight       int    `json:"weight"`
	SomeOtherID  int    `json:"some_other_id"`
}

// NEW: Search response structure
type SearchResponse struct {
	Products   []Product `json:"products"`
	TotalFound int       `json:"total_found"`
	SearchTime string    `json:"search_time"`
	Checked    int       `json:"checked"` // For verification
}

// NEW: Store with 100,000 products
type ProductStore struct {
	products []Product
	mu       sync.RWMutex
}

var store *ProductStore

// NEW: Generate 100,000 products at startup
func init() {
	store = &ProductStore{
		products: make([]Product, 100000),
	}

	log.Println("Generating 100,000 products...")

	brands := []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta", "Eta", "Theta"}
	categories := []string{"Electronics", "Books", "Home", "Clothing", "Sports", "Toys", "Food", "Garden"}

	for i := 0; i < 100000; i++ {
		store.products[i] = Product{
			ID:          i + 1,
			Name:        fmt.Sprintf("Product %s %d", brands[i%len(brands)], i+1),
			Category:    categories[i%len(categories)],
			Description: fmt.Sprintf("Description for product %d", i+1),
			Brand:       brands[i%len(brands)],
			// Keep fields from previous assignment if needed
			SKU:          fmt.Sprintf("SKU-%d", i+1),
			Manufacturer: brands[i%len(brands)],
			CategoryID:   (i % len(categories)) + 1,
			Weight:       100 + (i % 1000),
			SomeOtherID:  1000 + i,
		}
	}

	log.Println("✓ Generated 100,000 products")
}

// NEW: Search endpoint - checks exactly 100 products
func handleSearch(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	query := r.URL.Query().Get("q")

	if query == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Query parameter 'q' is required"})
		return
	}

	query = strings.ToLower(query)
	var results []Product
	checked := 0
	const maxCheck = 100 // CRITICAL: Only check 100 products
	const maxResults = 20

	store.mu.RLock()
	defer store.mu.RUnlock()

	// Check exactly 100 products
	for i := 0; i < len(store.products) && checked < maxCheck; i++ {
		checked++
		product := store.products[i]

		// Search in name and category (case-insensitive)
		if strings.Contains(strings.ToLower(product.Name), query) ||
			strings.Contains(strings.ToLower(product.Category), query) {
			if len(results) < maxResults {
				results = append(results, product)
			}
		}
	}

	searchTime := time.Since(start)

	response := SearchResponse{
		Products:   results,
		TotalFound: len(results),
		SearchTime: fmt.Sprintf("%.3fms", float64(searchTime.Microseconds())/1000.0),
		Checked:    checked,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// KEEP YOUR EXISTING health check
func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "healthy",
		"total_products": len(store.products),
	})
}

// KEEP YOUR EXISTING product endpoints if you want
// (GET /products/{id} and POST /products/{id}/details)
// ... your existing handlers ...

func main() {
	r := mux.NewRouter()

	// KEEP existing endpoints
	r.HandleFunc("/health", healthCheck).Methods("GET")

	// NEW: Add search endpoint
	r.HandleFunc("/products/search", handleSearch).Methods("GET")

	// KEEP your existing product endpoints if you have them
	// r.HandleFunc("/products/{productId}", getProduct).Methods("GET")
	// r.HandleFunc("/products/{productId}/details", addProductDetails).Methods("POST")

	port := "8080"
	log.Printf("Server starting on port %s", port)
	log.Printf("Total products loaded: %d", len(store.products))
	log.Fatal(http.ListenAndServe(":"+port, r))
}
