package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/gorilla/mux"
)

// Product represents the product schema from OpenAPI spec
type Product struct {
	ProductID    int    `json:"product_id"`
	SKU          string `json:"sku"`
	Manufacturer string `json:"manufacturer"`
	CategoryID   int    `json:"category_id"`
	Weight       int    `json:"weight"`
	SomeOtherID  int    `json:"some_other_id"`
}

// Error represents the error response schema
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// In-memory storage with mutex for thread safety
type ProductStore struct {
	sync.RWMutex
	products map[int]Product
}

var store = &ProductStore{
	products: make(map[int]Product),
}

// Validation functions
func validateProduct(p Product) *ErrorResponse {
	if p.ProductID < 1 {
		return &ErrorResponse{
			Error:   "INVALID_INPUT",
			Message: "Invalid product data",
			Details: "product_id must be a positive integer",
		}
	}
	if len(p.SKU) == 0 || len(p.SKU) > 100 {
		return &ErrorResponse{
			Error:   "INVALID_INPUT",
			Message: "Invalid product data",
			Details: "sku must be between 1 and 100 characters",
		}
	}
	if len(p.Manufacturer) == 0 || len(p.Manufacturer) > 200 {
		return &ErrorResponse{
			Error:   "INVALID_INPUT",
			Message: "Invalid product data",
			Details: "manufacturer must be between 1 and 200 characters",
		}
	}
	if p.CategoryID < 1 {
		return &ErrorResponse{
			Error:   "INVALID_INPUT",
			Message: "Invalid product data",
			Details: "category_id must be a positive integer",
		}
	}
	if p.Weight < 0 {
		return &ErrorResponse{
			Error:   "INVALID_INPUT",
			Message: "Invalid product data",
			Details: "weight must be non-negative",
		}
	}
	if p.SomeOtherID < 1 {
		return &ErrorResponse{
			Error:   "INVALID_INPUT",
			Message: "Invalid product data",
			Details: "some_other_id must be a positive integer",
		}
	}
	return nil
}

// GET /products/{productId}
func getProduct(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productID, err := strconv.Atoi(vars["productId"])

	if err != nil || productID < 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:   "INVALID_INPUT",
			Message: "Invalid product ID",
			Details: "Product ID must be a positive integer",
		})
		return
	}

	store.RLock()
	product, exists := store.products[productID]
	store.RUnlock()

	if !exists {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:   "NOT_FOUND",
			Message: "Product not found",
			Details: fmt.Sprintf("Product with ID %d does not exist", productID),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(product)
}

// POST /products/{productId}/details
func addProductDetails(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productID, err := strconv.Atoi(vars["productId"])

	if err != nil || productID < 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:   "INVALID_INPUT",
			Message: "Invalid product ID",
			Details: "Product ID must be a positive integer",
		})
		return
	}

	var product Product
	err = json.NewDecoder(r.Body).Decode(&product)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:   "INVALID_INPUT",
			Message: "Invalid JSON body",
			Details: err.Error(),
		})
		return
	}

	// Validate that product_id in body matches URL parameter
	if product.ProductID != productID {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:   "INVALID_INPUT",
			Message: "Product ID mismatch",
			Details: "Product ID in URL must match product_id in request body",
		})
		return
	}

	// Validate product fields
	if validationErr := validateProduct(product); validationErr != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(validationErr)
		return
	}

	// Store the product
	store.Lock()
	store.products[productID] = product
	store.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

// Health check endpoint
func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func main() {
	r := mux.NewRouter()

	// Product endpoints
	r.HandleFunc("/products/{productId}", getProduct).Methods("GET")
	r.HandleFunc("/products/{productId}/details", addProductDetails).Methods("POST")

	// Health check
	r.HandleFunc("/health", healthCheck).Methods("GET")

	port := "8080"
	log.Printf("Server starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
