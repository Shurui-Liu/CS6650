package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	_ "github.com/go-sql-driver/mysql"
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

type Cart struct {
	CartID     int64      `json:"cart_id"`
	CustomerID int        `json:"customer_id"`
	Status     string     `json:"status"`
	Items      []CartItem `json:"items"`
	CreatedAt  time.Time  `json:"created_at"`
}

type CartItem struct {
	ProductID int     `json:"product_id"`
	Quantity  int     `json:"quantity"`
	UnitPrice float64 `json:"unit_price"`
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

var snsClient *sns.Client
var sqsClient *sqs.Client
var snsTopicARN string
var sqsQueueURL string

var db *sql.DB

// ── Init ───────────────────────────────────────────────────────────────────────

func init() {
	store = &ProductStore{products: make([]Product, 100000)}
	log.Println("Generating 100,000 products...")
	brands := []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta", "Eta", "Theta"}
	categories := []string{"Electronics", "Books", "Home", "Clothing", "Sports", "Toys", "Food", "Garden"}
	for i := 0; i < 100000; i++ {
		store.products[i] = Product{
			ID:           i + 1,
			Name:         fmt.Sprintf("Product %s %d", brands[i%len(brands)], i+1),
			Category:     categories[i%len(categories)],
			Description:  fmt.Sprintf("Description for product %d", i+1),
			Brand:        brands[i%len(brands)],
			SKU:          fmt.Sprintf("SKU-%d", i+1),
			Manufacturer: brands[i%len(brands)],
			CategoryID:   (i % len(categories)) + 1,
			Weight:       100 + (i % 1000),
			SomeOtherID:  1000 + i,
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

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
		Products:   results,
		TotalFound: len(results),
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"order_id": order.OrderID,
		"status":   "pending",
		"message":  "Order received and queued for processing",
	})
}

// ── SQS Processor (optional background goroutine) ─────────────────────────────

func startOrderProcessor() {
	log.Println("✓ Order processor started — polling SQS...")
	for {
		out, err := sqsClient.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
			QueueUrl:            &sqsQueueURL,
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20,
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
	verifyPayment()
	order.Status = "completed"
	log.Printf("✓ Order %s completed", order.OrderID)
	sqsClient.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
		QueueUrl:      &sqsQueueURL,
		ReceiptHandle: msg.ReceiptHandle,
	})
}

// ── Shopping Cart Handlers ─────────────────────────────────────────────────────

// POST /shopping-carts
func handleCreateCart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CustomerID int `json:"customer_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if req.CustomerID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "customer_id is required"})
		return
	}

	res, err := db.ExecContext(r.Context(),
		"INSERT INTO carts (customer_id, status) VALUES (?, 'active')",
		req.CustomerID,
	)
	if err != nil {
		log.Printf("createCart: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create cart"})
		return
	}
	cartID, _ := res.LastInsertId()

	writeJSON(w, http.StatusCreated, Cart{
		CartID:     cartID,
		CustomerID: req.CustomerID,
		Status:     "active",
		Items:      []CartItem{},
		CreatedAt:  time.Now(),
	})
}

// GET /shopping-carts/{id}
func handleGetCart(w http.ResponseWriter, r *http.Request) {
	cartID, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cart id"})
		return
	}

	var cart Cart
	err = db.QueryRowContext(r.Context(),
		"SELECT cart_id, customer_id, status, created_at FROM carts WHERE cart_id = ?",
		cartID,
	).Scan(&cart.CartID, &cart.CustomerID, &cart.Status, &cart.CreatedAt)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "cart not found"})
		return
	}
	if err != nil {
		log.Printf("getCart: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve cart"})
		return
	}

	rows, err := db.QueryContext(r.Context(),
		"SELECT product_id, quantity, unit_price FROM cart_items WHERE cart_id = ?",
		cartID,
	)
	if err != nil {
		log.Printf("getCart items: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve cart items"})
		return
	}
	defer rows.Close()

	cart.Items = []CartItem{}
	for rows.Next() {
		var item CartItem
		if err := rows.Scan(&item.ProductID, &item.Quantity, &item.UnitPrice); err != nil {
			log.Printf("getCart scan: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read cart items"})
			return
		}
		cart.Items = append(cart.Items, item)
	}

	writeJSON(w, http.StatusOK, cart)
}

// POST /shopping-carts/{id}/items
func handleAddItem(w http.ResponseWriter, r *http.Request) {
	cartID, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cart id"})
		return
	}

	var item CartItem
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if item.ProductID <= 0 || item.Quantity <= 0 || item.UnitPrice < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "product_id, quantity (>0), and unit_price (>=0) are required"})
		return
	}

	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("addItem begin tx: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to start transaction"})
		return
	}
	defer tx.Rollback()

	// Lock the cart row — serializes concurrent writes to the same cart
	var status string
	err = tx.QueryRowContext(r.Context(),
		"SELECT status FROM carts WHERE cart_id = ? FOR UPDATE",
		cartID,
	).Scan(&status)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "cart not found"})
		return
	}
	if err != nil {
		log.Printf("addItem lock: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to lock cart"})
		return
	}
	if status != "active" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "cart is not active"})
		return
	}

	// Upsert: if product already in cart, add to quantity
	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO cart_items (cart_id, product_id, quantity, unit_price)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			quantity   = quantity + VALUES(quantity),
			unit_price = VALUES(unit_price)`,
		cartID, item.ProductID, item.Quantity, item.UnitPrice,
	)
	if err != nil {
		log.Printf("addItem upsert: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to add item"})
		return
	}

	_, err = tx.ExecContext(r.Context(),
		"UPDATE carts SET updated_at = NOW() WHERE cart_id = ?",
		cartID,
	)
	if err != nil {
		log.Printf("addItem update ts: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update cart"})
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("addItem commit: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to commit transaction"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ── Main ───────────────────────────────────────────────────────────────────────

func main() {
	var err error
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

	// ── MySQL connection pool ──────────────────────────────────────────────────
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		getEnv("DB_USER", "admin"),
		getEnv("DB_PASSWORD", ""),
		getEnv("DB_HOST", "localhost"),
		getEnv("DB_PORT", "3306"),
		getEnv("DB_NAME", "ordersdb"),
	)
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err = db.PingContext(context.Background()); err != nil {
		log.Printf("WARNING: DB not reachable at startup: %v", err)
	} else {
		log.Println("✓ Connected to MySQL")
	}

	if snsTopicARN == "" {
		log.Fatal("SNS_TOPIC_ARN environment variable is required")
	}

	// Only start SQS processor if queue URL is configured
	if sqsQueueURL != "" {
		go startOrderProcessor()
	} else {
		log.Println("SQS_QUEUE_URL not set — skipping local processor (using separate processor service)")
	}

	r := mux.NewRouter()
	r.HandleFunc("/health", healthCheck).Methods("GET")
	r.HandleFunc("/products/search", handleSearch).Methods("GET")
	r.HandleFunc("/orders/sync", handleOrdersSync).Methods("POST")
	r.HandleFunc("/orders/async", handleOrdersAsync).Methods("POST")
	r.HandleFunc("/shopping-carts", handleCreateCart).Methods("POST")
	r.HandleFunc("/shopping-carts/{id}", handleGetCart).Methods("GET")
	r.HandleFunc("/shopping-carts/{id}/items", handleAddItem).Methods("POST")

	port := getEnv("PORT", "8080")
	log.Printf("Server starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
