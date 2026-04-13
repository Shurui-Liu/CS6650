package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"

	"album-store/internal/cache"
	"album-store/internal/db"
	"album-store/internal/handler"
	"album-store/internal/queue"
	"album-store/internal/storage"
	"album-store/internal/worker"
)

func main() {
	// Load .env if present (ignored in production where env vars come from the env file).
	_ = godotenv.Load()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// ── Database ──────────────────────────────────────────────────────────────
	// Connect() uses DATABASE_URL (writer/primary) and DATABASE_READER_URL
	// (replica). Falls back to primary if replica is not configured.
	pools, err := db.Connect(ctx)
	if err != nil {
		log.Fatalf("db.Connect: %v", err)
	}
	defer pools.Close()

	if err := db.Migrate(ctx, pools.Writer); err != nil {
		log.Fatalf("db.Migrate: %v", err)
	}

	queries := db.NewQueries(pools)

	// ── AWS ───────────────────────────────────────────────────────────────────
	// On EC2, config.LoadDefaultConfig picks up credentials from the instance
	// metadata service automatically — no static keys needed.
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(mustEnv("AWS_REGION")),
	)
	if err != nil {
		log.Fatalf("aws config: %v", err)
	}

	s3Base := mustEnv("S3_BASE_URL")
	s3Client := storage.New(awsCfg, mustEnv("S3_BUCKET"), s3Base)
	sqsClient := queue.New(awsCfg, mustEnv("SQS_QUEUE_URL"))

	// ── Redis ─────────────────────────────────────────────────────────────────
	// nil if REDIS_ADDR is unset — cache package handles nil gracefully.
	rdb := cache.New()

	// ── Worker ────────────────────────────────────────────────────────────────
	// API instances set WORKER_CONCURRENCY=0 to skip the worker loop entirely.
	// Dedicated worker instances set it to a non-zero value (default 20).
	concurrency := envInt("WORKER_CONCURRENCY", 0)
	if concurrency > 0 {
		w := worker.New(queries, sqsClient, s3Client, s3Base, concurrency)
		go w.Run(ctx)
	}

	// ── Router ────────────────────────────────────────────────────────────────
	// Worker instances may have PORT unset — skip HTTP listener in that case.
	port := os.Getenv("PORT")
	if port == "" {
		// No HTTP server: run as a pure worker process.
		log.Println("PORT not set — running as worker only")
		<-ctx.Done()
		log.Println("shutting down...")
		return
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	// Health never touches the DB.
	r.Get("/health", handler.Health)

	albumH := handler.NewAlbumHandler(queries, s3Client, rdb)
	photoH := handler.NewPhotoHandler(queries, s3Client, sqsClient, s3Base)

	r.Route("/albums", func(r chi.Router) {
		r.Get("/", albumH.List)
		r.Post("/", albumH.Create)
		r.Get("/{albumId}", albumH.Get)
		r.Put("/{albumId}", albumH.Upsert)
		r.Delete("/{albumId}", albumH.Delete)
		r.Post("/{albumId}/photos", photoH.Upload)
		r.Get("/{albumId}/photos", photoH.List)
		r.Get("/{albumId}/photos/{photoId}", photoH.Get)
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", port),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("server listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown: %v", err)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
