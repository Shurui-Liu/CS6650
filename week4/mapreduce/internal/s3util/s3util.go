package s3util

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Path struct {
	Bucket string
	Key    string
}

func ParseS3URL(s string) (S3Path, error) {
	if !strings.HasPrefix(s, "s3://") {
		return S3Path{}, errors.New("s3 url must start with s3://")
	}
	u, err := url.Parse(s)
	if err != nil {
		return S3Path{}, err
	}
	b := u.Host
	k := strings.TrimPrefix(u.Path, "/")
	if b == "" || k == "" {
		return S3Path{}, errors.New("invalid s3 url; need bucket and key")
	}
	return S3Path{Bucket: b, Key: k}, nil
}

func GetObjectBytes(ctx context.Context, client *s3.Client, p S3Path) ([]byte, error) {
	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &p.Bucket,
		Key:    &p.Key,
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()

	return io.ReadAll(out.Body)
}

func PutObjectBytes(ctx context.Context, client *s3.Client, p S3Path, body []byte, contentType string) error {
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &p.Bucket,
		Key:         &p.Key,
		Body:        strings.NewReader(string(body)),
		ContentType: &contentType,
	})
	return err
}
