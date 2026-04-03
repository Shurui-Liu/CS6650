package main

import (
	"log"
	"net/http"
	"os"
	"strings"
)

// valuePayload is the external API contract for clients.
type valuePayload struct {
	Value string `json:"value"`
}

// replicationPayload is the internal contract between nodes.
// The logical version lets each node discard stale writes when a newer
// coordinator has already written a higher-versioned value.
type replicationPayload struct {
	Value   string `json:"value"`
	Version int64  `json:"version"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// PEERS is a comma-separated list of the *other* node URLs.
	// This node is not in its own peer list.
	var peers []string
	for _, addr := range strings.Split(os.Getenv("PEERS"), ",") {
		if addr = strings.TrimSpace(addr); addr != "" {
			peers = append(peers, addr)
		}
	}

	log.Printf("leaderless node starting on :%s | N=%d | peers: %v", port, len(peers)+1, peers)

	store := NewKVStore()
	node := NewNode(store, peers)
	mux := http.NewServeMux()

	// Client-facing endpoints: reads and writes go to any node.
	mux.HandleFunc("/kv/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			node.HandleSet(w, r)
		case http.MethodGet:
			node.HandleGet(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Internal endpoint: replication from a coordinator node.
	mux.HandleFunc("/internal/kv/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			node.HandlePeerSet(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
