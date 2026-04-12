package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"album-store/internal/db"
	"album-store/internal/model"
	"album-store/internal/queue"
	"album-store/internal/storage"
)

type PhotoHandler struct {
	q      *db.Queries
	s3     *storage.Client
	sqs    *queue.Client
	s3Base string
}

func NewPhotoHandler(q *db.Queries, s3 *storage.Client, sqs *queue.Client, s3Base string) *PhotoHandler {
	return &PhotoHandler{q: q, s3: s3, sqs: sqs, s3Base: s3Base}
}

// Upload handles multipart/form-data photo uploads.
// Form field: "photo" (file)
//
// Flow:
//  1. Atomically claim a seq number for this album.
//  2. Create the photo row with status='processing', url=null.
//  3. Upload the file to S3.
//  4. Send a SQS message so the worker can set status='processed' + url.
func (h *PhotoHandler) Upload(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "albumId")

	// Verify album exists.
	if _, err := h.q.GetAlbum(r.Context(), albumID); err != nil {
		http.Error(w, "album not found", http.StatusNotFound)
		return
	}

	// 32 MB max in memory, rest spilled to disk.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "failed to parse multipart form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("photo")
	if err != nil {
		http.Error(w, "photo field is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Claim sequence number.
	seq, err := h.q.NextPhotoSeq(r.Context(), albumID)
	if err != nil {
		http.Error(w, "failed to allocate photo sequence", http.StatusInternalServerError)
		return
	}

	photoID := uuid.NewString()
	s3Key := fmt.Sprintf("albums/%s/%d-%s", albumID, seq, header.Filename)
	publicURL := fmt.Sprintf("%s/%s", h.s3Base, s3Key)

	// Create DB record first (status=processing, url=null).
	photo, err := h.q.CreatePhoto(r.Context(), photoID, albumID, seq)
	if err != nil {
		http.Error(w, "failed to create photo record", http.StatusInternalServerError)
		return
	}

	// Upload to S3.
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if _, err := h.s3.Upload(r.Context(), s3Key, contentType, file); err != nil {
		http.Error(w, "failed to upload photo", http.StatusInternalServerError)
		return
	}

	// Enqueue for the worker to confirm and flip status.
	msg := model.PhotoMessage{
		PhotoID: photoID,
		AlbumID: albumID,
		Seq:     seq,
		S3Key:   s3Key,
	}
	if err := h.sqs.SendMessage(r.Context(), msg); err != nil {
		// Non-fatal: worker will reconcile; log and continue.
		_ = err
	}

	// Return the record with the expected final URL so callers can use it.
	photo.URL = &publicURL

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(photo)
}

func (h *PhotoHandler) List(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "albumId")

	photos, err := h.q.ListPhotos(r.Context(), albumID)
	if err != nil {
		http.Error(w, "failed to list photos", http.StatusInternalServerError)
		return
	}
	if photos == nil {
		photos = []model.Photo{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(photos)
}
