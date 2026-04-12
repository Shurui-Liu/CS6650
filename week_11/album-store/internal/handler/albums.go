package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"album-store/internal/db"
	"album-store/internal/model"
	"album-store/internal/storage"
)

type AlbumHandler struct {
	q  *db.Queries
	s3 *storage.Client
}

func NewAlbumHandler(q *db.Queries, s3 *storage.Client) *AlbumHandler {
	return &AlbumHandler{q: q, s3: s3}
}

// List returns every album ever created, newest first. No pagination.
func (h *AlbumHandler) List(w http.ResponseWriter, r *http.Request) {
	albums, err := h.q.ListAlbums(r.Context())
	if err != nil {
		http.Error(w, "failed to list albums", http.StatusInternalServerError)
		return
	}
	if albums == nil {
		albums = []model.Album{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(albums)
}

// Create handles POST /albums — generates a new album_id.
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

// Get handles GET /albums/{albumId}.
func (h *AlbumHandler) Get(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "albumId")

	album, err := h.q.GetAlbum(r.Context(), albumID)
	if err != nil {
		http.Error(w, "album not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(album)
}

// Upsert handles PUT /albums/{albumId} — INSERT … ON CONFLICT DO UPDATE.
func (h *AlbumHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "albumId")

	var req model.CreateAlbumRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Title == "" || req.Owner == "" {
		http.Error(w, "title and owner are required", http.StatusBadRequest)
		return
	}

	album, err := h.q.UpsertAlbum(r.Context(), albumID, req)
	if err != nil {
		http.Error(w, "failed to upsert album", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(album)
}

// Delete handles DELETE /albums/{albumId}.
// Removes both the DB row and all associated S3 objects within 5 seconds.
func (h *AlbumHandler) Delete(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "albumId")

	deleteCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Collect S3 keys before touching the DB.
	keys, err := h.q.ListPhotoS3Keys(deleteCtx, albumID)
	if err != nil {
		http.Error(w, "failed to list photo keys", http.StatusInternalServerError)
		return
	}

	// Delete S3 objects (batch, up to 1000).
	if err := h.s3.DeleteObjects(deleteCtx, keys); err != nil {
		http.Error(w, "failed to delete S3 objects", http.StatusInternalServerError)
		return
	}

	// Delete DB rows — photos first (FK), then album.
	if err := h.q.DeletePhotosForAlbum(deleteCtx, albumID); err != nil {
		http.Error(w, "failed to delete photos", http.StatusInternalServerError)
		return
	}
	if err := h.q.DeleteAlbum(deleteCtx, albumID); err != nil {
		http.Error(w, "album not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
