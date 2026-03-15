package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// Item represents a line item in an order
type Item struct {
	ProductID int     `json:"product_id"`
	Name      string  `json:"name"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
}

// Order represents an e-commerce order (pending, processing, completed)
type Order struct {
	OrderID    string    `json:"order_id"`
	CustomerID int       `json:"customer_id"`
	Status     string    `json:"status"`
	Items      []Item    `json:"items"`
	CreatedAt  time.Time `json:"created_at"`
}

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

// Payment verification bottleneck: buffered channel as semaphore.
// Capacity 15 = at most 15 orders in verification; each takes 3s → 15/3 = 5 orders/sec.
const (
	paymentVerifyDuration = 3 * time.Second
	ordersPerSecond       = 5
	paymentSlotsCapacity  = 15 // 5 orders/sec * 3 sec per order
)
var paymentSlots chan struct{}

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

	// Payment verification semaphore: limits concurrent verifications so we get real backpressure
	paymentSlots = make(chan struct{}, paymentSlotsCapacity)
	for i := 0; i < paymentSlotsCapacity; i++ {
		paymentSlots <- struct{}{}
	}
	log.Printf("✓ Payment verification bottleneck: %d slots, %v each → ~%d orders/sec\n",
		paymentSlotsCapacity, paymentVerifyDuration, ordersPerSecond)
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

// handleOrdersSync processes an order synchronously: acquires a payment slot, verifies (3s), returns.
// The buffered channel ensures real backpressure—only 15 verifications can run at once.
func handleOrdersSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	var order Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON body"})
		return
	}

	if order.OrderID == "" {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		order.OrderID = "ord-" + hex.EncodeToString(b)
	}
	if order.CreatedAt.IsZero() {
		order.CreatedAt = time.Now()
	}
	order.Status = "processing"

	// Acquire a payment slot (blocks if all 15 are in use — real bottleneck)
	<-paymentSlots
	defer func() { paymentSlots <- struct{}{} }()

	// Simulate payment verification: 3 second delay (thread is blocked for this request)
	time.Sleep(paymentVerifyDuration)

	order.Status = "completed"

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(order)
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

	// Phase 1: Synchronous order processing (payment verification 3s, ~5 orders/sec via semaphore)
	r.HandleFunc("/orders/sync", handleOrdersSync).Methods("POST")

	// KEEP your existing product endpoints if you have them
	// r.HandleFunc("/products/{productId}", getProduct).Methods("GET")
	// r.HandleFunc("/products/{productId}/details", addProductDetails).Methods("POST")

	port := "8080"
	log.Printf("Server starting on port %s", port)
	log.Printf("Total products loaded: %d", len(store.products))
	log.Fatal(http.ListenAndServe(":"+port, r))
}
