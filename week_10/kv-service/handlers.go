package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

type valuePayload struct {
	Value string `json:"value"`
}

func extractKey(r *http.Request) string {
	// path: /kv/{key}
	return strings.TrimPrefix(r.URL.Path, "/kv/")
}

func handleSet(store *KVStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := extractKey(r)
		if key == "" {
			http.Error(w, "key cannot be empty", http.StatusBadRequest)
			return
		}

		var payload valuePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}

		store.Set(key, payload.Value)
		w.WriteHeader(http.StatusCreated)
	}
}

func handleGet(store *KVStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := extractKey(r)
		if key == "" {
			http.Error(w, "key cannot be empty", http.StatusBadRequest)
			return
		}

		val, ok := store.Get(key)
		if !ok {
			http.Error(w, "key not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(valuePayload{Value: val})
	}
}

func newRouter(store *KVStore) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/kv/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			handleSet(store)(w, r)
		case http.MethodGet:
			handleGet(store)(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}
