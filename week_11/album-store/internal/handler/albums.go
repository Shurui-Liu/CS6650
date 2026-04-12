package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"album-store/internal/db"
	"album-store/internal/model"
)

type AlbumHandler struct {
	q *db.Queries
}

func NewAlbumHandler(q *db.Queries) *AlbumHandler {
	return &AlbumHandler{q: q}
}

func (h *AlbumHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req model.CreateAlbumRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Title == "" || req.Owner == "" {
		http.Error(w, "title and owner are required", http.StatusBadRequest)
		return
	}

	album, err := h.q.CreateAlbum(r.Context(), uuid.NewString(), req)
	if err != nil {
		http.Error(w, "failed to create album", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(album)
}

func (h *AlbumHandler) Get(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "albumId")
	if albumID == "" {
		http.Error(w, "invalid album id", http.StatusBadRequest)
		return
	}

	album, err := h.q.GetAlbum(r.Context(), albumID)
	if err != nil {
		http.Error(w, "album not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(album)
}
