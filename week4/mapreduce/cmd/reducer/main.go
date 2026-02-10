package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"mapreduce/internal/s3util"
)

type reduceResponse struct {
	Output string `json:"output"`
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

	http.HandleFunc("/reduce", func(w http.ResponseWriter, r *http.Request) {
		u1 := r.URL.Query().Get("u1")
		u2 := r.URL.Query().Get("u2")
		u3 := r.URL.Query().Get("u3")
		if u1 == "" || u2 == "" || u3 == "" {
			http.Error(w, "missing u1/u2/u3", http.StatusBadRequest)
			return
		}

		urls := []string{u1, u2, u3}
		total := map[string]int{}
		var bucket string

		for _, u := range urls {
			p, err := s3util.ParseS3URL(u)
			if err != nil {
				http.Error(w, "bad s3 url: "+err.Error(), http.StatusBadRequest)
				return
			}
			bucket = p.Bucket

			data, err := s3util.GetObjectBytes(r.Context(), s3c, p)
			if err != nil {
				http.Error(w, "s3 get: "+err.Error(), http.StatusInternalServerError)
				return
			}

			part := map[string]int{}
			if err := json.Unmarshal(data, &part); err != nil {
				http.Error(w, "bad mapper json: "+err.Error(), http.StatusBadRequest)
				return
			}

			for k, v := range part {
				total[k] += v
			}
		}

		outKey := "final/result.json"
		b, _ := json.Marshal(total)

		err := s3util.PutObjectBytes(
			r.Context(),
			s3c,
			s3util.S3Path{Bucket: bucket, Key: outKey},
			b,
			"application/json",
		)
		if err != nil {
			http.Error(w, "s3 put: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reduceResponse{Output: "s3://" + bucket + "/" + outKey})
	})

	log.Println("reducer listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
