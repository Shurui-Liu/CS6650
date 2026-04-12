package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type Client struct {
	svc      *sqs.Client
	queueURL string
}

func New(cfg aws.Config, queueURL string) *Client {
	return &Client{
		svc:      sqs.NewFromConfig(cfg),
		queueURL: queueURL,
	}
}

func (c *Client) SendMessage(ctx context.Context, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	_, err = c.svc.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    &c.queueURL,
		MessageBody: aws.String(string(b)),
	})
	return err
}

type Message struct {
	ID            string
	ReceiptHandle string
	Body          string
}

func (c *Client) ReceiveMessages(ctx context.Context, maxMessages int32) ([]Message, error) {
	out, err := c.svc.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            &c.queueURL,
		MaxNumberOfMessages: maxMessages,
		WaitTimeSeconds:     20, // long polling
	})
	if err != nil {
		return nil, err
	}
	msgs := make([]Message, 0, len(out.Messages))
	for _, m := range out.Messages {
		msgs = append(msgs, Message{
			ID:            aws.ToString(m.MessageId),
			ReceiptHandle: aws.ToString(m.ReceiptHandle),
			Body:          aws.ToString(m.Body),
		})
	}
	return msgs, nil
}

func (c *Client) DeleteMessage(ctx context.Context, receiptHandle string) error {
	_, err := c.svc.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      &c.queueURL,
		ReceiptHandle: &receiptHandle,
	})
	return err
}

func (c *Client) DeleteMessageBatch(ctx context.Context, msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}
	entries := make([]types.DeleteMessageBatchRequestEntry, len(msgs))
	for i, m := range msgs {
		entries[i] = types.DeleteMessageBatchRequestEntry{
			Id:            aws.String(m.ID),
			ReceiptHandle: aws.String(m.ReceiptHandle),
		}
	}
	_, err := c.svc.DeleteMessageBatch(ctx, &sqs.DeleteMessageBatchInput{
		QueueUrl: &c.queueURL,
		Entries:  entries,
	})
	return err
}
