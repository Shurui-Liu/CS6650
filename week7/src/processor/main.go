package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
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

// SNS wraps the actual message inside a "Message" field
type SNSWrapper struct {
	Message string `json:"Message"`
}

const paymentVerifyDuration = 3 * time.Second

// ── Payment processor ──────────────────────────────────────────────────────────

func processOrder(order Order) {
	log.Printf("[worker] Processing order %s for customer %d", order.OrderID, order.CustomerID)
	time.Sleep(paymentVerifyDuration) // simulate payment verification
	log.Printf("[worker] Completed order %s", order.OrderID)
}

// ── SQS poll loop ──────────────────────────────────────────────────────────────

func pollForever(client *sqs.Client, queueURL string) {
	log.Printf("Polling SQS queue: %s", queueURL)

	for {
		// Long poll: wait up to 20s for messages, get up to 10 at once
		out, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20, // long polling — reduces empty responses & cost
		})
		if err != nil {
			log.Printf("ERROR receiving messages: %v — retrying in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, msg := range out.Messages {
			// Capture for goroutine
			m := msg

			go func() {
				// SQS receives SNS-wrapped JSON: unwrap first
				var wrapper SNSWrapper
				if err := json.Unmarshal([]byte(*m.Body), &wrapper); err != nil {
					log.Printf("ERROR unmarshalling SNS wrapper: %v", err)
					return
				}

				var order Order
				if err := json.Unmarshal([]byte(wrapper.Message), &order); err != nil {
					log.Printf("ERROR unmarshalling order: %v", err)
					return
				}

				processOrder(order)

				// Delete message from SQS after successful processing
				_, delErr := client.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
					QueueUrl:      aws.String(queueURL),
					ReceiptHandle: m.ReceiptHandle,
				})
				if delErr != nil {
					log.Printf("ERROR deleting message %s: %v", *m.MessageId, delErr)
				}
			}()
		}
	}
}

// ── Main ───────────────────────────────────────────────────────────────────────

func main() {
	queueURL := os.Getenv("SQS_QUEUE_URL")
	if queueURL == "" {
		log.Fatal("SQS_QUEUE_URL environment variable is required")
	}

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(os.Getenv("AWS_REGION")),
	)
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}

	client := sqs.NewFromConfig(cfg)
	pollForever(client, queueURL)
}
