package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type Item struct {
	ProductID string  `json:"product_id"`
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

type SNSEnvelope struct {
	Message string `json:"Message"`
}

var sqsClient *sqs.Client
var queueURL string

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func init() {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(getEnv("AWS_REGION", "us-west-2")),
	)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}
	sqsClient = sqs.NewFromConfig(cfg)
	queueURL = getEnv("SQS_QUEUE_URL", "")
	if queueURL == "" {
		log.Fatal("SQS_QUEUE_URL env var not set")
	}
}

func processOrder(order Order) {
	log.Printf("[START] OrderID=%s CustomerID=%d", order.OrderID, order.CustomerID)
	time.Sleep(3 * time.Second) // simulate payment
	log.Printf("[DONE]  OrderID=%s status=completed", order.OrderID)
}

func handleMessage(msg types.Message) {
	var envelope SNSEnvelope
	if err := json.Unmarshal([]byte(*msg.Body), &envelope); err != nil {
		log.Printf("failed to unwrap SNS envelope: %v", err)
		return
	}
	var order Order
	if err := json.Unmarshal([]byte(envelope.Message), &order); err != nil {
		log.Printf("failed to parse order: %v", err)
		return
	}

	processOrder(order)

	_, err := sqsClient.DeleteMessage(context.TODO(), &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(queueURL),
		ReceiptHandle: msg.ReceiptHandle,
	})
	if err != nil {
		log.Printf("failed to delete message: %v", err)
	}
}

// worker continuously polls SQS and spawns goroutines per message
func worker(id int, sem chan struct{}) {
	log.Printf("Worker %d started", id)
	for {
		out, err := sqsClient.ReceiveMessage(context.TODO(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20,
		})
		if err != nil {
			log.Printf("Worker %d: ReceiveMessage error: %v", id, err)
			time.Sleep(2 * time.Second)
			continue
		}
		for _, msg := range out.Messages {
			sem <- struct{}{} // acquire semaphore slot
			go func(m types.Message) {
				defer func() { <-sem }() // release slot when done
				handleMessage(m)
			}(msg)
		}
	}
}

func main() {
	numWorkers := getEnvInt("WORKER_COUNT", 1)
	log.Printf("Starting processor with %d workers (3s payment each → ~%.1f orders/sec)",
		numWorkers, float64(numWorkers)/3.0)

	// Semaphore limits total concurrent goroutines across all workers
	sem := make(chan struct{}, numWorkers)

	// Start multiple polling workers
	for i := 1; i <= numWorkers; i++ {
		go worker(i, sem)
	}

	// Block forever
	select {}
}
