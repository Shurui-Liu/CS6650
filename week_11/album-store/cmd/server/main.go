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

	"album-store/internal/db"
	"album-store/internal/handler"
	"album-store/internal/queue"
	"album-store/internal/storage"
	"album-store/internal/worker"
)

func main() {
	// Load .env if present (ignored in production where env vars are set directly).
	_ = godotenv.Load()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// ── Database ──────────────────────────────────────────────────────────────
	databaseURL := mustEnv("DATABASE_URL")
	pool, err := db.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("db.New: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("db.Migrate: %v", err)
	}

	queries := db.NewQueries(pool)

	// ── AWS ───────────────────────────────────────────────────────────────────
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(mustEnv("AWS_REGION")),
	)
	if err != nil {
		log.Fatalf("aws config: %v", err)
	}

	s3Base := mustEnv("S3_BASE_URL")
	s3Client := storage.New(awsCfg, mustEnv("S3_BUCKET"), s3Base)
	sqsClient := queue.New(awsCfg, mustEnv("SQS_QUEUE_URL"))

	// ── Worker ────────────────────────────────────────────────────────────────
	concurrency := envInt("WORKER_CONCURRENCY", 20)
	w := worker.New(queries, sqsClient, s3Base, concurrency)
	go w.Run(ctx)

	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	r.Get("/health", handler.Health)

	albumH := handler.NewAlbumHandler(queries)
	photoH := handler.NewPhotoHandler(queries, s3Client, sqsClient, s3Base)

	r.Route("/albums", func(r chi.Router) {
		r.Post("/", albumH.Create)
		r.Get("/{albumId}", albumH.Get)
		r.Post("/{albumId}/photos", photoH.Upload)
		r.Get("/{albumId}/photos", photoH.List)
	})

	// ── HTTP server ───────────────────────────────────────────────────────────
	port := envString("PORT", "8080")
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

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
