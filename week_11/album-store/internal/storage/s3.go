package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Client struct {
	svc        *s3.Client
	bucket     string
	baseURL    string
}

func New(cfg aws.Config, bucket, baseURL string) *Client {
	return &Client{
		svc:     s3.NewFromConfig(cfg),
		bucket:  bucket,
		baseURL: baseURL,
	}
}

// Upload streams r to S3 under key and returns the public URL.
func (c *Client) Upload(ctx context.Context, key, contentType string, r io.Reader) (string, error) {
	_, err := c.svc.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &c.bucket,
		Key:         &key,
		Body:        r,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("s3 put: %w", err)
	}
	return fmt.Sprintf("%s/%s", c.baseURL, key), nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.svc.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &c.bucket,
		Key:    &key,
	})
	return err
}
