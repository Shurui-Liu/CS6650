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

// PUT /internal/kv/{key} — replication from leader; sleep 100ms before responding.
func (f *Follower) HandleInternalSet(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/internal/kv/")
	if key == "" {
		http.Error(w, "key cannot be empty", http.StatusBadRequest)
		return
	}

	var payload valuePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	time.Sleep(100 * time.Millisecond) // simulate storage write delay
	f.store.Set(key, payload.Value)
	w.WriteHeader(http.StatusCreated)
}

// GET /internal/kv/{key} — read request from leader; sleep 50ms before responding.
func (f *Follower) HandleInternalGet(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/internal/kv/")
	if key == "" {
		http.Error(w, "key cannot be empty", http.StatusBadRequest)
		return
	}

	time.Sleep(50 * time.Millisecond) // simulate read delay
	val, ok := f.store.Get(key)
	if !ok {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(valuePayload{Value: val})
}

// GET /kv/{key} — direct client read; serves local (possibly stale) data.
func (f *Follower) HandleGet(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/kv/")
	if key == "" {
		http.Error(w, "key cannot be empty", http.StatusBadRequest)
		return
	}

	val, ok := f.store.Get(key)
	if !ok {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(valuePayload{Value: val})
}
