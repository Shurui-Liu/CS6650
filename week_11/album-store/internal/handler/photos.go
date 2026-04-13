package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Buffer the entire file into memory.
	// ParseMultipartForm already read it (up to 32 MB); io.ReadAll just drains
	// the remaining bytes. Using bytes.Reader gives the SDK a seekable body with
	// a known length, which is required for S3 PutObject.
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "failed to read file", http.StatusInternalServerError)
		return
	}

	isLarge := len(data) > maxDirectUploadBytes

	var currentKey string
	if isLarge {
		// Large file: stage in tmp/ first so the SQS message stays under 256 KB.
		// Worker will copy it to the final key.
		currentKey = fmt.Sprintf("tmp/%s", photoID)
	} else {
		currentKey = finalKey
	}

	// Create DB record synchronously so GET /photos/{photoId} returns 200
	// with status="processing" immediately after the 202 response.
	photo, err := h.q.CreatePhoto(r.Context(), photoID, albumID, finalKey, seq)
	if err != nil {
		http.Error(w, "failed to create photo record", http.StatusInternalServerError)
		return
	}

	// Return 202 immediately — do not block on S3 upload.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(photo)

	// Upload to S3 in the background, then either mark completed directly
	// (small files already at final key) or enqueue for the worker (large
	// files that need a tmp→final copy). This avoids SQS round-trip latency
	// for the common case and dramatically improves POST→completed p95.
	go func() {
		ctx := context.Background()
		reader := bytes.NewReader(data)
		if _, err := h.s3.Upload(ctx, currentKey, contentType, reader, int64(len(data))); err != nil {
			log.Printf("s3 upload error (key=%s): %v", currentKey, err)
			return
		}
		if isLarge {
			// Large file: worker must copy tmp/→albums/ before marking done.
			msg := model.PhotoMessage{
				PhotoID:    photoID,
				AlbumID:    albumID,
				Seq:        seq,
				CurrentKey: currentKey,
				FinalKey:   finalKey,
			}
			if err := h.sqs.SendMessage(ctx, msg); err != nil {
				log.Printf("sqs send error (photo=%s): %v", photoID, err)
			}
		} else {
			// Small file: already at final key — mark completed directly,
			// no worker or SQS round-trip needed.
			publicURL := fmt.Sprintf("%s/%s", h.s3Base, finalKey)
			if err := h.q.MarkPhotoProcessed(ctx, photoID, publicURL); err != nil {
				log.Printf("mark processed error (photo=%s): %v", photoID, err)
			}
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

	// Best-effort S3 cleanup — don't fail the request if S3 delete errors.
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
