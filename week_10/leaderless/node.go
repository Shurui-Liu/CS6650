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

type Node struct {
	store *KVStore
	peers []string // base URLs of the other N-1 nodes
}

func NewNode(store *KVStore, peers []string) *Node {
	return &Node{store: store, peers: peers}
}

// HandleSet handles PUT /kv/{key} from a client.
//
// This node becomes the Write Coordinator for the request:
//  1. Write locally (increments the logical version for this key).
//  2. Replicate to every peer, sleeping 200ms after each send (W=N).
//  3. Return 201-Created only after all peers have acknowledged.
func (n *Node) HandleSet(w http.ResponseWriter, r *http.Request) {
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

	// Write locally; the coordinator owns version assignment.
	version := n.store.SetCoordinator(key, payload.Value)
	confirmed := 1 // this node counts as the first write

	body, _ := json.Marshal(replicationPayload{Value: payload.Value, Version: version})

	for _, peer := range n.peers {
		if err := n.replicateToPeer(peer, key, body); err == nil {
			confirmed++
		}
		time.Sleep(200 * time.Millisecond) // simulated inter-node delay
	}

	if confirmed == len(n.peers)+1 {
		w.WriteHeader(http.StatusCreated)
	} else {
		http.Error(w,
			fmt.Sprintf("only %d/%d nodes written", confirmed, len(n.peers)+1),
			http.StatusInternalServerError)
	}
}

// HandleGet handles GET /kv/{key} from a client.
//
// R=1: the node returns its own local value immediately, with no fan-out.
// If replication has not yet reached this node, the value may be stale —
// this is the intentional inconsistency window.
func (n *Node) HandleGet(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/kv/")
	if key == "" {
		http.Error(w, "key cannot be empty", http.StatusBadRequest)
		return
	}

	entry, ok := n.store.Get(key)
	if !ok {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(valuePayload{Value: entry.Value})
}

// HandlePeerSet handles PUT /internal/kv/{key} from another node acting as coordinator.
//
// Sleeps 100ms to simulate storage write latency before acknowledging.
func (n *Node) HandlePeerSet(w http.ResponseWriter, r *http.Request) {
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
	n.store.SetPeer(key, payload.Value, payload.Version)
	w.WriteHeader(http.StatusCreated)
}

func (n *Node) replicateToPeer(peer, key string, body []byte) error {
	url := fmt.Sprintf("%s/internal/kv/%s", peer, key)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("coordinator: build request to %s: %v", peer, err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		log.Printf("coordinator: replication to %s failed: %v", peer, err)
		return err
	}
	resp.Body.Close()
	log.Printf("coordinator: replicated key=%q to %s", key, peer)
	return nil
}
