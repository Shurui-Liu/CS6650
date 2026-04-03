package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type Leader struct {
	store     *KVStore
	followers []string // base URLs, e.g. ["http://follower1:8080"]
}

func NewLeader(store *KVStore, followers []string) *Leader {
	return &Leader{store: store, followers: followers}
}

// PUT /kv/{key}
func (l *Leader) HandleSet(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/kv/")
	if key == "" {
		http.Error(w, "key cannot be empty", http.StatusBadRequest)
		return
	}

	var payload valuePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Write locally first.
	l.store.Set(key, payload.Value)

	// Replicate to each follower: send then sleep 200ms.
	body, _ := json.Marshal(payload)
	for _, follower := range l.followers {
		url := fmt.Sprintf("%s/internal/kv/%s", follower, key)
		resp, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
		if err != nil {
			log.Printf("leader: error building request to %s: %v", follower, err)
		} else {
			resp.Header.Set("Content-Type", "application/json")
			client := &http.Client{Timeout: 5 * time.Second}
			res, err := client.Do(resp)
			if err != nil {
				log.Printf("leader: replication to %s failed: %v", follower, err)
			} else {
				res.Body.Close()
				log.Printf("leader: replicated key=%q to %s", key, follower)
			}
		}
		// Sleep 200ms after each follower message regardless of success.
		time.Sleep(200 * time.Millisecond)
	}

	w.WriteHeader(http.StatusCreated)
}

// GET /kv/{key} — no sleep on the leader side
func (l *Leader) HandleGet(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/kv/")
	if key == "" {
		http.Error(w, "key cannot be empty", http.StatusBadRequest)
		return
	}

	val, ok := l.store.Get(key)
	if !ok {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(valuePayload{Value: val})
}
