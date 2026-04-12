package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"album-store/internal/db"
	"album-store/internal/model"
	"album-store/internal/queue"
	"album-store/internal/storage"
)

const maxDirectUploadBytes = 200 * 1024 // 200 KB

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
//  1. Verify album exists and claim a seq number.
//  2. Determine S3 routing: files ≤ 200 KB go to the final key directly;
//     files > 200 KB go to tmp/{photoID} first (SQS 256 KB cap).
//  3. Upload to S3.
//  4. Create the photo DB row (status=processing, url=null, s3_key=finalKey).
//  5. Send SQS message with current and final keys for the worker.
func (h *PhotoHandler) Upload(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "albumId")

	if _, err := h.q.GetAlbum(r.Context(), albumID); err != nil {
		http.Error(w, "album not found", http.StatusNotFound)
		return
	}

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

	// Claim sequence number atomically.
	seq, err := h.q.NextPhotoSeq(r.Context(), albumID)
	if err != nil {
		http.Error(w, "failed to allocate photo sequence", http.StatusInternalServerError)
		return
	}

	photoID := uuid.NewString()
	finalKey := fmt.Sprintf("albums/%s/%d-%s", albumID, seq, header.Filename)

	// Peek at the first maxDirectUploadBytes+1 bytes to determine routing.
	peek := make([]byte, maxDirectUploadBytes+1)
	n, _ := io.ReadFull(file, peek)
	isLarge := n > maxDirectUploadBytes

	// Reconstruct a reader from peeked bytes + remainder.
	reader := io.MultiReader(bytes.NewReader(peek[:n]), file)

	var currentKey string
	if isLarge {
		currentKey = fmt.Sprintf("tmp/%s", photoID)
	} else {
		currentKey = finalKey
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	if _, err := h.s3.Upload(r.Context(), currentKey, contentType, reader); err != nil {
		http.Error(w, "failed to upload photo", http.StatusInternalServerError)
		return
	}

	// Store the final key in DB so DELETE can clean up S3 correctly.
	photo, err := h.q.CreatePhoto(r.Context(), photoID, albumID, finalKey, seq)
	if err != nil {
		http.Error(w, "failed to create photo record", http.StatusInternalServerError)
		return
	}

	msg := model.PhotoMessage{
		PhotoID:    photoID,
		AlbumID:    albumID,
		Seq:        seq,
		CurrentKey: currentKey,
		FinalKey:   finalKey,
	}
	if err := h.sqs.SendMessage(r.Context(), msg); err != nil {
		// Non-fatal: worker will reconcile.
		_ = err
	}

	// Return the expected final URL in the response.
	finalURL := fmt.Sprintf("%s/%s", h.s3Base, finalKey)
	photo.URL = &finalURL

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
