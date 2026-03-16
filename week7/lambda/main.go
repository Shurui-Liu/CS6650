package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
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

func handler(ctx context.Context, snsEvent events.SNSEvent) error {
	for _, record := range snsEvent.Records {
		msg := record.SNS.Message

		var order Order
		if err := json.Unmarshal([]byte(msg), &order); err != nil {
			log.Printf("Failed to parse order: %v", err)
			continue
		}

		log.Printf("[START] OrderID=%s CustomerID=%d", order.OrderID, order.CustomerID)

		// Same 3-second payment simulation
		time.Sleep(3 * time.Second)

		order.Status = "completed"
		log.Printf("[DONE] OrderID=%s status=completed", order.OrderID)
	}
	return nil
}

func main() {
	lambda.Start(handler)
}
