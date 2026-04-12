package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"album-store/internal/db"
	"album-store/internal/model"
	"album-store/internal/queue"
)

type Worker struct {
	q           *db.Queries
	sqs         *queue.Client
	s3Base      string
	concurrency int
}

func New(q *db.Queries, sqs *queue.Client, s3Base string, concurrency int) *Worker {
	return &Worker{q: q, sqs: sqs, s3Base: s3Base, concurrency: concurrency}
}

// Run polls SQS and processes messages until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	sem := make(chan struct{}, w.concurrency)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msgs, err := w.sqs.ReceiveMessages(ctx, 10)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("worker: receive error: %v", err)
			continue
		}

		var wg sync.WaitGroup
		for _, msg := range msgs {
			sem <- struct{}{}
			wg.Add(1)
			go func(m queue.Message) {
				defer wg.Done()
				defer func() { <-sem }()
				if err := w.process(ctx, m); err != nil {
					log.Printf("worker: process %s: %v", m.ID, err)
					return
				}
				if err := w.sqs.DeleteMessage(ctx, m.ReceiptHandle); err != nil {
					log.Printf("worker: delete %s: %v", m.ID, err)
				}
			}(msg)
		}
		wg.Wait()
	}
}

func (w *Worker) process(ctx context.Context, m queue.Message) error {
	var msg model.PhotoMessage
	if err := json.Unmarshal([]byte(m.Body), &msg); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	publicURL := fmt.Sprintf("%s/%s", w.s3Base, msg.S3Key)
	if err := w.q.MarkPhotoProcessed(ctx, msg.PhotoID, publicURL); err != nil {
		return fmt.Errorf("mark processed: %w", err)
	}

	log.Printf("worker: processed photo %s (album %s, seq %d)", msg.PhotoID, msg.AlbumID, msg.Seq)
	return nil
}
