package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type Follower struct {
	store *KVStore
}

func NewFollower(store *KVStore) *Follower {
	return &Follower{store: store}
}

// HandleInternalSet handles PUT /internal/kv/{key} — replication from the leader.
// Sleeps 100ms to simulate storage write latency before responding.
func (f *Follower) HandleInternalSet(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/internal/kv/")
	if key == "" {
		http.Error(w, "key cannot be empty", http.StatusBadRequest)
		return
	}

	var payload replicationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	time.Sleep(100 * time.Millisecond) // simulate storage write delay
	f.store.SetFollower(key, payload.Value, payload.Version)
	w.WriteHeader(http.StatusCreated)
}

// HandleInternalGet handles GET /internal/kv/{key} — read request from the leader.
// Sleeps 50ms to simulate read latency, then returns the value with its version.
func (f *Follower) HandleInternalGet(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/internal/kv/")
	if key == "" {
		http.Error(w, "key cannot be empty", http.StatusBadRequest)
		return
	}

	time.Sleep(50 * time.Millisecond) // simulate read delay
	entry, ok := f.store.Get(key)
	if !ok {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(replicationPayload{Value: entry.Value, Version: entry.Version})
}

// HandleLocalRead handles GET /local_read/{key}.
// Testing-only endpoint: returns this follower's raw local value with no delays.
// During an in-flight write the value may be stale — that is the point.
func (f *Follower) HandleLocalRead(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/local_read/")
	if key == "" {
		http.Error(w, "key cannot be empty", http.StatusBadRequest)
		return
	}
	entry, ok := f.store.Get(key)
	if !ok {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(valuePayload{Value: entry.Value})
}

// HandleGet handles GET /kv/{key} — direct client read from this follower.
// Returns the local (possibly stale) value with no added delay.
func (f *Follower) HandleGet(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/kv/")
	if key == "" {
		http.Error(w, "key cannot be empty", http.StatusBadRequest)
		return
	}

	entry, ok := f.store.Get(key)
	if !ok {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(valuePayload{Value: entry.Value})
}
