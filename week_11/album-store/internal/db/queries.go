package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"album-store/internal/model"
)

type Queries struct {
	pool *pgxpool.Pool
}

func NewQueries(pool *pgxpool.Pool) *Queries {
	return &Queries{pool: pool}
}

func (q *Queries) CreateAlbum(ctx context.Context, albumID string, r model.CreateAlbumRequest) (model.Album, error) {
	var a model.Album
	err := q.pool.QueryRow(ctx,
		`INSERT INTO albums (album_id, title, description, owner)
		 VALUES ($1, $2, $3, $4)
		 RETURNING album_id, title, description, owner, photo_seq, created_at`,
		albumID, r.Title, r.Description, r.Owner,
	).Scan(&a.AlbumID, &a.Title, &a.Description, &a.Owner, &a.PhotoSeq, &a.CreatedAt)
	return a, err
}

func (q *Queries) GetAlbum(ctx context.Context, albumID string) (model.Album, error) {
	var a model.Album
	err := q.pool.QueryRow(ctx,
		`SELECT album_id, title, description, owner, photo_seq, created_at
		 FROM albums WHERE album_id = $1`,
		albumID,
	).Scan(&a.AlbumID, &a.Title, &a.Description, &a.Owner, &a.PhotoSeq, &a.CreatedAt)
	return a, err
}

// NextPhotoSeq atomically increments photo_seq and returns the new value.
func (q *Queries) NextPhotoSeq(ctx context.Context, albumID string) (int, error) {
	var seq int
	err := q.pool.QueryRow(ctx,
		`UPDATE albums SET photo_seq = photo_seq + 1
		 WHERE album_id = $1
		 RETURNING photo_seq`,
		albumID,
	).Scan(&seq)
	return seq, err
}

// CreatePhoto inserts a photo record with status='processing'.
// url is initially nil; the worker sets it after confirming the S3 upload.
func (q *Queries) CreatePhoto(ctx context.Context, photoID, albumID string, seq int) (model.Photo, error) {
	var p model.Photo
	err := q.pool.QueryRow(ctx,
		`INSERT INTO photos (photo_id, album_id, seq, status)
		 VALUES ($1, $2, $3, 'processing')
		 RETURNING photo_id, album_id, seq, status, url, created_at`,
		photoID, albumID, seq,
	).Scan(&p.PhotoID, &p.AlbumID, &p.Seq, &p.Status, &p.URL, &p.CreatedAt)
	return p, err
}

func (q *Queries) ListPhotos(ctx context.Context, albumID string) ([]model.Photo, error) {
	rows, err := q.pool.Query(ctx,
		`SELECT photo_id, album_id, seq, status, url, created_at
		 FROM photos WHERE album_id = $1 ORDER BY seq`,
		albumID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var photos []model.Photo
	for rows.Next() {
		var p model.Photo
		if err := rows.Scan(&p.PhotoID, &p.AlbumID, &p.Seq, &p.Status, &p.URL, &p.CreatedAt); err != nil {
			return nil, err
		}
		photos = append(photos, p)
	}
	return photos, rows.Err()
}

// MarkPhotoProcessed sets status='processed' and the public S3 URL.
func (q *Queries) MarkPhotoProcessed(ctx context.Context, photoID, url string) error {
	_, err := q.pool.Exec(ctx,
		`UPDATE photos SET status = 'processed', url = $2 WHERE photo_id = $1`,
		photoID, url,
	)
	return err
}
