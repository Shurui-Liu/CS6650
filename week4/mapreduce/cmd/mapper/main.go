package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"mapreduce/internal/s3util"
)

var wordRe = regexp.MustCompile(`[A-Za-z0-9']+`)

type mapResponse struct {
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

	http.HandleFunc("/map", func(w http.ResponseWriter, r *http.Request) {
		in := r.URL.Query().Get("s3")
		if in == "" {
			http.Error(w, "missing query param: s3", http.StatusBadRequest)
			return
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

		counts := map[string]int{}
		for _, token := range wordRe.FindAllString(string(data), -1) {
			token = strings.ToLower(token)
			counts[token]++
		}

		// Stable ordering helps debugging
		keys := make([]string, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		ordered := make(map[string]int, len(counts))
		for _, k := range keys {
			ordered[k] = counts[k]
		}

		// Output name based on chunk key
		// e.g., chunks/chunk-0.txt => maps/chunk-0.json
		base := strings.TrimPrefix(p.Key, "chunks/")
		base = strings.TrimSuffix(base, ".txt")
		outKey := "maps/" + base + ".json"

		b, _ := json.Marshal(ordered)
		err = s3util.PutObjectBytes(
			r.Context(),
			s3c,
			s3util.S3Path{Bucket: p.Bucket, Key: outKey},
			b,
			"application/json",
		)
		if err != nil {
			http.Error(w, "s3 put: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mapResponse{Output: "s3://" + p.Bucket + "/" + outKey})
	})

	log.Println("mapper listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
