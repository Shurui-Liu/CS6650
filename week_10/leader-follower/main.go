package main

import (
	"log"
	"net/http"
	"os"
	"strings"
)

type valuePayload struct {
	Value string `json:"value"`
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
			addr = strings.TrimSpace(addr)
			if addr != "" {
				followers = append(followers, addr)
			}
		}
		log.Printf("starting as LEADER on :%s with followers: %v", port, followers)

		l := NewLeader(store, followers)
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

	case "follower":
		log.Printf("starting as FOLLOWER on :%s", port)

		f := NewFollower(store)
		// Public client reads
		mux.HandleFunc("/kv/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				f.HandleGet(w, r)
			} else {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		})
		// Internal replication / read endpoints used by the leader
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

	default:
		log.Fatalf("ROLE must be 'leader' or 'follower', got %q", role)
	}

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
