package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"album-store/internal/db"
	"album-store/internal/model"
	"album-store/internal/queue"
	"album-store/internal/storage"
)

// maxConcurrentUploads caps simultaneous S3 streaming uploads.
// Each slot uses at most one 5 MB part buffer; keeping this bounded
// prevents memory and goroutine explosion under large concurrent load.
const maxConcurrentUploads = 20

type PhotoHandler struct {
	q         *db.Queries
	s3        *storage.Client
	sqs       *queue.Client
	s3Base    string
	uploadSem chan struct{}
}

func NewPhotoHandler(q *db.Queries, s3 *storage.Client, sqs *queue.Client, s3Base string) *PhotoHandler {
	return &PhotoHandler{
		q:         q,
		s3:        s3,
		sqs:       sqs,
		s3Base:    s3Base,
		uploadSem: make(chan struct{}, maxConcurrentUploads),
	}
}

// Upload handles multipart/form-data photo uploads.
//
// Flow:
//  1. Verify album exists and parse multipart headers.
//  2. Claim a seq number and create the DB record (status=processing).
//  3. Acquire the upload semaphore (caps concurrent S3 transfers).
//  4. Stream the photo body directly to S3 — no full-body RAM buffer.
//  5. Return 202 (body is now fully consumed; safe to respond).
//  6. Mark the photo completed in a background goroutine (S3 is already done).
func (h *PhotoHandler) Upload(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "albumId")

	if _, err := h.q.GetAlbumPrimary(r.Context(), albumID); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		return
	}

	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || params["boundary"] == "" {
		http.Error(w, "invalid multipart content-type", http.StatusBadRequest)
		return
	}

	mr := multipart.NewReader(r.Body, params["boundary"])
	var part *multipart.Part
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		if p.FormName() == "photo" {
			part = p
			break
		}
		io.Copy(io.Discard, p)
	}
	if part == nil {
		http.Error(w, "photo field is required", http.StatusBadRequest)
		return
	}

	filename := part.FileName()
	if filename == "" {
		filename = "photo"
	}
	fileCT := part.Header.Get("Content-Type")
	if fileCT == "" {
		fileCT = "application/octet-stream"
	}

	// Claim sequence number atomically.
	seq, err := h.q.NextPhotoSeq(r.Context(), albumID)
	if err != nil {
		http.Error(w, "failed to allocate photo sequence", http.StatusInternalServerError)
		return
	}

	photoID := uuid.NewString()
	finalKey := fmt.Sprintf("albums/%s/%d-%s", albumID, seq, filename)

	photo, err := h.q.CreatePhoto(r.Context(), photoID, albumID, finalKey, seq)
	if err != nil {
		http.Error(w, "failed to create photo record", http.StatusInternalServerError)
		return
	}

	// Acquire semaphore before touching S3. Released when this handler returns.
	// Prevents unbounded concurrent large uploads from exhausting memory/goroutines.
	select {
	case h.uploadSem <- struct{}{}:
		defer func() { <-h.uploadSem }()
	case <-r.Context().Done():
		io.Copy(io.Discard, r.Body)
		http.Error(w, "request cancelled", http.StatusServiceUnavailable)
		return
	}

	// Stream the multipart part directly to S3 in 5 MB chunks — peak memory
	// per upload is one chunk, regardless of total file size.
	publicURL, err := h.s3.UploadStream(r.Context(), finalKey, fileCT, part)
	if err != nil {
		log.Printf("s3 stream upload error (key=%s): %v", finalKey, err)
		http.Error(w, "failed to upload photo", http.StatusInternalServerError)
		return
	}

	// Body fully consumed — safe to respond 202.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(photo)

	// S3 upload is already done; just update the DB status.
	go func() {
		if err := h.q.MarkPhotoProcessed(context.Background(), photoID, publicURL); err != nil {
			log.Printf("mark processed error (photo=%s): %v", photoID, err)
		}
	}()
}

func (h *PhotoHandler) Delete(w http.ResponseWriter, r *http.Request) {
	photoID := chi.URLParam(r, "photoId")

	s3Key, err := h.q.DeletePhoto(r.Context(), photoID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		return
	}

	if s3Key != "" {
		_ = h.s3.Delete(r.Context(), s3Key)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *PhotoHandler) Get(w http.ResponseWriter, r *http.Request) {
	photoID := chi.URLParam(r, "photoId")

	photo, err := h.q.GetPhoto(r.Context(), photoID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
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
