package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/gorilla/mux"
)

// ── Types ──────────────────────────────────────────────────────────────────────

type Item struct {
	ProductID int     `json:"product_id"`
	Name      string  `json:"name"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
}

type Order struct {
	OrderID    string    `json:"order_id"`
	CustomerID int       `json:"customer_id"`
	Status     string    `json:"status"`
	Items      []Item    `json:"items"`
	CreatedAt  time.Time `json:"created_at"`
}

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

type SearchResponse struct {
	Products   []Product `json:"products"`
	TotalFound int       `json:"total_found"`
	SearchTime string    `json:"search_time"`
	Checked    int       `json:"checked"`
}

type ProductStore struct {
	products []Product
	mu       sync.RWMutex
}

// ── Globals ────────────────────────────────────────────────────────────────────

var store *ProductStore

const (
	paymentVerifyDuration = 3 * time.Second
	ordersPerSecond       = 5
	paymentSlotsCapacity  = 15
)

var paymentSlots chan struct{}

// AWS clients (initialised in main)
var snsClient *sns.Client
var sqsClient *sqs.Client
var snsTopicARN string
var sqsQueueURL string

// ── Init ───────────────────────────────────────────────────────────────────────

func init() {
	store = &ProductStore{products: make([]Product, 100000)}
	log.Println("Generating 100,000 products...")
	brands := []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta", "Eta", "Theta"}
	categories := []string{"Electronics", "Books", "Home", "Clothing", "Sports", "Toys", "Food", "Garden"}
	for i := 0; i < 100000; i++ {
		store.products[i] = Product{
			ID: i + 1, Name: fmt.Sprintf("Product %s %d", brands[i%len(brands)], i+1),
			Category: categories[i%len(categories)], Description: fmt.Sprintf("Description for product %d", i+1),
			Brand: brands[i%len(brands)], SKU: fmt.Sprintf("SKU-%d", i+1),
			Manufacturer: brands[i%len(brands)], CategoryID: (i % len(categories)) + 1,
			Weight: 100 + (i % 1000), SomeOtherID: 1000 + i,
		}
	}
	log.Println("✓ Generated 100,000 products")

	paymentSlots = make(chan struct{}, paymentSlotsCapacity)
	for i := 0; i < paymentSlotsCapacity; i++ {
		paymentSlots <- struct{}{}
	}
	log.Printf("✓ Payment slots: %d @ %v each → ~%d orders/sec\n",
		paymentSlotsCapacity, paymentVerifyDuration, ordersPerSecond)
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func newOrderID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "ord-" + hex.EncodeToString(b)
}

func verifyPayment() {
	<-paymentSlots
	defer func() { paymentSlots <- struct{}{} }()
	time.Sleep(paymentVerifyDuration)
}

// ── Handlers ───────────────────────────────────────────────────────────────────

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "healthy",
		"total_products": len(store.products),
	})
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	query := strings.ToLower(r.URL.Query().Get("q"))
	if query == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Query parameter 'q' is required"})
		return
	}
	var results []Product
	checked := 0
	store.mu.RLock()
	defer store.mu.RUnlock()
	for i := 0; i < len(store.products) && checked < 100; i++ {
		checked++
		p := store.products[i]
		if strings.Contains(strings.ToLower(p.Name), query) ||
			strings.Contains(strings.ToLower(p.Category), query) {
			if len(results) < 20 {
				results = append(results, p)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SearchResponse{
		Products: results, TotalFound: len(results),
		SearchTime: fmt.Sprintf("%.3fms", float64(time.Since(start).Microseconds())/1000.0),
		Checked:    checked,
	})
}

// Phase 1: synchronous — blocks until payment done
func handleOrdersSync(w http.ResponseWriter, r *http.Request) {
	var order Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON body"})
		return
	}
	if order.OrderID == "" {
		order.OrderID = newOrderID()
	}
	if order.CreatedAt.IsZero() {
		order.CreatedAt = time.Now()
	}
	order.Status = "processing"
	verifyPayment()
	order.Status = "completed"
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(order)
}

// Phase 3: async — publish to SNS, return 202 immediately
func handleOrdersAsync(w http.ResponseWriter, r *http.Request) {
	var order Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON body"})
		return
	}
	if order.OrderID == "" {
		order.OrderID = newOrderID()
	}
	order.CreatedAt = time.Now()
	order.Status = "pending"

	// Serialize and publish to SNS
	body, err := json.Marshal(order)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	msg := string(body)
	_, err = snsClient.Publish(context.Background(), &sns.PublishInput{
		TopicArn: &snsTopicARN,
		Message:  &msg,
	})
	if err != nil {
		log.Printf("SNS publish error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to queue order"})
		return
	}

	// Return 202 immediately — customer is NOT waiting for payment
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"order_id": order.OrderID,
		"status":   "pending",
		"message":  "Order received and queued for processing",
	})
}

// ── SQS Processor (runs in background goroutine) ───────────────────────────────
// Continuously polls SQS, spawns a goroutine per message to simulate payment.

func startOrderProcessor() {
	log.Println("✓ Order processor started — polling SQS...")
	for {
		out, err := sqsClient.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
			QueueUrl:            &sqsQueueURL,
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20, // long polling
		})
		if err != nil {
			log.Printf("SQS receive error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, msg := range out.Messages {
			go processMessage(msg)
		}
	}
}

func processMessage(msg sqstypes.Message) {
	// SQS wraps SNS messages — unwrap the SNS envelope
	var snsEnvelope struct {
		Message string `json:"Message"`
	}
	if err := json.Unmarshal([]byte(*msg.Body), &snsEnvelope); err != nil {
		log.Printf("Failed to parse SNS envelope: %v", err)
		return
	}

	var order Order
	if err := json.Unmarshal([]byte(snsEnvelope.Message), &order); err != nil {
		log.Printf("Failed to parse order: %v", err)
		return
	}

	log.Printf("Processing order %s for customer %d", order.OrderID, order.CustomerID)

	// Simulate payment verification (3s) — but this is background, not blocking the customer
	verifyPayment()

	order.Status = "completed"
	log.Printf("✓ Order %s completed", order.OrderID)

	// Delete message from SQS after successful processing
	sqsClient.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
		QueueUrl:      &sqsQueueURL,
		ReceiptHandle: msg.ReceiptHandle,
	})
}

// ── Main ───────────────────────────────────────────────────────────────────────

func main() {
	// Load AWS config (uses ECS task role automatically)
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(getEnv("AWS_REGION", "us-west-2")),
	)
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}

	snsClient = sns.NewFromConfig(cfg)
	sqsClient = sqs.NewFromConfig(cfg)
	snsTopicARN = getEnv("SNS_TOPIC_ARN", "")
	sqsQueueURL = getEnv("SQS_QUEUE_URL", "")

	if snsTopicARN == "" || sqsQueueURL == "" {
		log.Fatal("SNS_TOPIC_ARN and SQS_QUEUE_URL environment variables are required")
	}

	// Start SQS polling in background
	go startOrderProcessor()

	r := mux.NewRouter()
	r.HandleFunc("/health", healthCheck).Methods("GET")
	r.HandleFunc("/products/search", handleSearch).Methods("GET")
	r.HandleFunc("/orders/sync", handleOrdersSync).Methods("POST")
	r.HandleFunc("/orders/async", handleOrdersAsync).Methods("POST") // NEW

	port := getEnv("PORT", "8080")
	log.Printf("Server starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
