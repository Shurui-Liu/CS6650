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
	W         int      // write quorum
	R         int      // read quorum
}

func NewLeader(store *KVStore, followers []string, w, r int) *Leader {
	return &Leader{store: store, followers: followers, W: w, R: r}
}

// HandleSet implements PUT /kv/{key}.
//
// Write flow:
//   - Leader writes locally and counts that as 1.
//   - Replicates to followers one by one, sleeping 200ms after each send.
//   - Responds to the client as soon as W total acks are collected.
//   - Any remaining followers are replicated asynchronously in the background.
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

	// Write locally; returns the new monotonically increasing version for this key.
	version := l.store.SetLeader(key, payload.Value)
	confirmed := 1 // leader itself counts as the first write

	body, _ := json.Marshal(replicationPayload{Value: payload.Value, Version: version})

	if l.W == 1 {
		// W=1: respond immediately, replicate everything in the background.
		w.WriteHeader(http.StatusCreated)
		go l.replicateAll(key, body)
		return
	}

	// Replicate synchronously until we reach the write quorum, then handle the
	// rest asynchronously so the client is not blocked longer than necessary.
	var pending []string
	for _, follower := range l.followers {
		if confirmed >= l.W {
			// Quorum already met — defer this follower to the background.
			pending = append(pending, follower)
			continue
		}
		if err := l.replicateOne(follower, key, body); err == nil {
			confirmed++
		}
		time.Sleep(200 * time.Millisecond) // simulated inter-node delay
	}

	if confirmed >= l.W {
		w.WriteHeader(http.StatusCreated)
	} else {
		http.Error(w,
			fmt.Sprintf("write quorum not met: %d/%d acks", confirmed, l.W),
			http.StatusInternalServerError)
	}

	if len(pending) > 0 {
		go func() {
			for _, follower := range pending {
				l.replicateOne(follower, key, body) //nolint:errcheck
				time.Sleep(200 * time.Millisecond)
			}
		}()
	}
}

// HandleGet implements GET /kv/{key}.
//
// Read flow:
//   - R=1: return the leader's local value immediately (no follower contact).
//   - R>1: fan out to the leader's store plus (R-1) followers in parallel;
//     return the entry with the highest version number.
func (l *Leader) HandleGet(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/kv/")
	if key == "" {
		http.Error(w, "key cannot be empty", http.StatusBadRequest)
		return
	}

	if l.R == 1 {
		entry, ok := l.store.Get(key)
		if !ok {
			http.Error(w, "key not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(valuePayload{Value: entry.Value})
		return
	}

	// Fan out to R nodes: leader (local) + up to R-1 followers.
	followersToRead := l.R - 1
	if followersToRead > len(l.followers) {
		followersToRead = len(l.followers)
	}

	type result struct {
		entry KVEntry
		ok    bool
	}

	ch := make(chan result, followersToRead)
	for i := 0; i < followersToRead; i++ {
		go func(addr string) {
			entry, ok := l.readFromFollower(addr, key)
			ch <- result{entry, ok}
		}(l.followers[i])
	}

	// Seed with leader's local value.
	best, found := l.store.Get(key)

	for i := 0; i < followersToRead; i++ {
		res := <-ch
		if res.ok && (!found || res.entry.Version > best.Version) {
			best = res.entry
			found = true
		}
	}

	if !found {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(valuePayload{Value: best.Value})
}

// replicateOne sends a single replication PUT to one follower.
func (l *Leader) replicateOne(follower, key string, body []byte) error {
	url := fmt.Sprintf("%s/internal/kv/%s", follower, key)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("leader: build request to %s: %v", follower, err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		log.Printf("leader: replication to %s failed: %v", follower, err)
		return err
	}
	resp.Body.Close()
	log.Printf("leader: replicated key=%q to %s", key, follower)
	return nil
}

// replicateAll sends to all followers sequentially with the 200ms inter-node delay.
func (l *Leader) replicateAll(key string, body []byte) {
	for _, follower := range l.followers {
		l.replicateOne(follower, key, body) //nolint:errcheck
		time.Sleep(200 * time.Millisecond)
	}
}

// readFromFollower calls GET /internal/kv/{key} on a follower (which adds 50ms delay).
func (l *Leader) readFromFollower(follower, key string) (KVEntry, bool) {
	url := fmt.Sprintf("%s/internal/kv/%s", follower, key)
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		log.Printf("leader: read from %s failed: %v", follower, err)
		return KVEntry{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return KVEntry{}, false
	}
	var p replicationPayload
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return KVEntry{}, false
	}
	return KVEntry{Value: p.Value, Version: p.Version}, true
}
