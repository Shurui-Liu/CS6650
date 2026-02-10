package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"mapreduce/internal/s3util"
)

type splitResponse struct {
	Chunks []string `json:"chunks"`
}

func main() {
	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("load aws config: %v", err)
	}
	s3c := s3.NewFromConfig(cfg)

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	http.HandleFunc("/split", func(w http.ResponseWriter, r *http.Request) {
		in := r.URL.Query().Get("s3")
		if in == "" {
			http.Error(w, "missing query param: s3", http.StatusBadRequest)
			return
		}

		parts := 3
		if pStr := r.URL.Query().Get("parts"); pStr != "" {
			if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
				parts = p
			}
		}

		p, err := s3util.ParseS3URL(in)
		if err != nil {
			http.Error(w, "bad s3 url: "+err.Error(), http.StatusBadRequest)
			return
		}

		data, err := s3util.GetObjectBytes(r.Context(), s3c, p)
		if err != nil {
			http.Error(w, "s3 get: "+err.Error(), http.StatusInternalServerError)
			return
		}

		lines := strings.Split(string(data), "\n")
		chunkSize := (len(lines) + parts - 1) / parts

		var out []string
		for i := 0; i < parts; i++ {
			start := i * chunkSize
			if start >= len(lines) {
				break
			}
			end := (i + 1) * chunkSize
			if end > len(lines) {
				end = len(lines)
			}

			chunkText := strings.Join(lines[start:end], "\n")
			key := "chunks/chunk-" + strconv.Itoa(i) + ".txt"

			err = s3util.PutObjectBytes(
				r.Context(),
				s3c,
				s3util.S3Path{Bucket: p.Bucket, Key: key},
				[]byte(chunkText),
				"text/plain",
			)
			if err != nil {
				http.Error(w, "s3 put: "+err.Error(), http.StatusInternalServerError)
				return
			}

			out = append(out, "s3://"+p.Bucket+"/"+key)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(splitResponse{Chunks: out})
	})

	log.Println("splitter listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
