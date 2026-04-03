package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// valuePayload is the external API contract for clients.
type valuePayload struct {
	Value string `json:"value"`
}

// replicationPayload is the internal contract between leader and followers.
// It carries the logical version so followers can detect and discard stale writes,
// and so the leader can pick the most recent value during a quorum read.
type replicationPayload struct {
	Value   string `json:"value"`
	Version int64  `json:"version"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	role := os.Getenv("ROLE") // "leader" or "follower"
	store := NewKVStore()
	mux := http.NewServeMux()

	switch role {
	case "leader":
		followerAddrs := strings.Split(os.Getenv("FOLLOWERS"), ",")
		var followers []string
		for _, addr := range followerAddrs {
			if addr = strings.TrimSpace(addr); addr != "" {
				followers = append(followers, addr)
			}
		}
		// W and R default to N=5 (synchronous all-node write, single-node read).
		w := mustEnvInt("W", 5)
		r := mustEnvInt("R", 1)
		log.Printf("starting as LEADER on :%s | N=%d W=%d R=%d | followers: %v",
			port, len(followers)+1, w, r, followers)

		l := NewLeader(store, followers, w, r)
		mux.HandleFunc("/kv/", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPut:
				l.HandleSet(w, r)
			case http.MethodGet:
				l.HandleGet(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		})
		mux.HandleFunc("/local_read/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				l.HandleLocalRead(w, r)
			} else {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		})

	case "follower":
		log.Printf("starting as FOLLOWER on :%s", port)

		f := NewFollower(store)
		// Public endpoint: clients may read directly from any follower.
		mux.HandleFunc("/kv/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				f.HandleGet(w, r)
			} else {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		})
		// Internal endpoints: used exclusively by the leader for replication and quorum reads.
		mux.HandleFunc("/internal/kv/", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPut:
				f.HandleInternalSet(w, r)
			case http.MethodGet:
				f.HandleInternalGet(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		})
		// Testing-only endpoint: raw local read, no delays or quorum.
		mux.HandleFunc("/local_read/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				f.HandleLocalRead(w, r)
			} else {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		})

	default:
		log.Fatalf("ROLE must be 'leader' or 'follower', got %q", role)
	}

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func mustEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("invalid %s=%q: %v", key, v, err)
	}
	return n
}
