package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Client struct {
	svc     *s3.Client
	bucket  string
	baseURL string
}

func New(cfg aws.Config, bucket, baseURL string) *Client {
	return &Client{
		svc:     s3.NewFromConfig(cfg),
		bucket:  bucket,
		baseURL: baseURL,
	}
}

// Upload streams r to S3 under key and returns the public URL.
// contentLength must be the exact byte size of r; the AWS SDK requires it
// when the body is a non-seekable io.Reader (e.g. io.MultiReader).
func (c *Client) Upload(ctx context.Context, key, contentType string, r io.Reader, contentLength int64) (string, error) {
	_, err := c.svc.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        &c.bucket,
		Key:           &key,
		Body:          r,
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(contentLength),
	})
	if err != nil {
		return "", fmt.Errorf("s3 put: %w", err)
	}
	return fmt.Sprintf("%s/%s", c.baseURL, key), nil
}

// UploadStream uploads r to S3 under key using multipart upload.
// Unlike Upload, it does not require the caller to know the content length and
// never buffers the entire body in memory — it reads and uploads in 5 MB chunks.
// Use this for large or unknown-size payloads.
func (c *Client) UploadStream(ctx context.Context, key, contentType string, r io.Reader) (string, error) {
	uploader := manager.NewUploader(c.svc, func(u *manager.Uploader) {
		u.PartSize = 5 * 1024 * 1024 // 5 MB per part
		u.Concurrency = 1            // sequential parts — one goroutine per upload
	})
	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      &c.bucket,
		Key:         &key,
		Body:        r,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("s3 upload stream: %w", err)
	}
	return fmt.Sprintf("%s/%s", c.baseURL, key), nil
}

// Copy copies an object within the same bucket (used to move tmp/ → albums/).
func (c *Client) Copy(ctx context.Context, srcKey, dstKey string) error {
	copySource := fmt.Sprintf("%s/%s", c.bucket, srcKey)
	_, err := c.svc.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     &c.bucket,
		CopySource: aws.String(copySource),
		Key:        aws.String(dstKey),
	})
	if err != nil {
		return fmt.Errorf("s3 copy %s → %s: %w", srcKey, dstKey, err)
	}
	return nil
}

// Delete removes a single object.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.svc.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &c.bucket,
		Key:    &key,
	})
	return err
}

// DeleteObjects removes up to 1000 objects in a single API call.
// Silently succeeds if keys is empty.
func (c *Client) DeleteObjects(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	objs := make([]types.ObjectIdentifier, len(keys))
	for i, k := range keys {
		objs[i] = types.ObjectIdentifier{Key: aws.String(k)}
	}
	_, err := c.svc.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: &c.bucket,
		Delete: &types.Delete{Objects: objs, Quiet: aws.Bool(true)},
	})
	return err
}
