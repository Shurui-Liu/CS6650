package model

import "time"

type Album struct {
	AlbumID     string    `json:"album_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Owner       string    `json:"owner"`
	PhotoSeq    int       `json:"photo_seq"`
	CreatedAt   time.Time `json:"created_at"`
}

type Photo struct {
	PhotoID   string    `json:"photo_id"`
	AlbumID   string    `json:"album_id"`
	Seq       int       `json:"seq"`
	Status    string    `json:"status"` // "processing" | "processed"
	URL       *string   `json:"url"`    // null until worker sets it
	CreatedAt time.Time `json:"created_at"`
}

type CreateAlbumRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Owner       string `json:"owner"`
}

type PhotoMessage struct {
	PhotoID string `json:"photo_id"`
	AlbumID string `json:"album_id"`
	Seq     int    `json:"seq"`
	S3Key   string `json:"s3_key"`
}
