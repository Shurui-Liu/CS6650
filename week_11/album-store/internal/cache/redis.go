package cache

import (
	"context"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

const TTL = 30 * time.Second

// New returns a Redis client configured from REDIS_ADDR.
// Returns nil if REDIS_ADDR is unset — all cache functions handle nil gracefully.
func New() *redis.Client {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		return nil
	}
	return redis.NewClient(&redis.Options{Addr: addr})
}

func GetAlbum(ctx context.Context, r *redis.Client, albumID string) (string, bool) {
	if r == nil {
		return "", false
	}
	val, err := r.Get(ctx, "album:"+albumID).Result()
	return val, err == nil
}

func SetAlbum(ctx context.Context, r *redis.Client, albumID, json string) {
	if r == nil {
		return
	}
	r.Set(ctx, "album:"+albumID, json, TTL)
}

// InvalidateAlbum removes the single-album and the list cache entries.
// Called on every PUT /albums/:id so readers see fresh data.
func InvalidateAlbum(ctx context.Context, r *redis.Client, albumID string) {
	if r == nil {
		return
	}
	r.Del(ctx, "album:"+albumID, "albums:list")
}

func GetAlbumList(ctx context.Context, r *redis.Client) (string, bool) {
	if r == nil {
		return "", false
	}
	val, err := r.Get(ctx, "albums:list").Result()
	return val, err == nil
}

func SetAlbumList(ctx context.Context, r *redis.Client, json string) {
	if r == nil {
		return
	}
	r.Set(ctx, "albums:list", json, TTL)
}
