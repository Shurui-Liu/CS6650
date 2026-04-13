package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"album-store/internal/model"
)

// Queries routes each operation to the correct pool:
//   - writer (primary) for INSERT / UPDATE / DELETE
//   - reader (replica) for SELECT-only operations
type Queries struct {
	writer *pgxpool.Pool
	reader *pgxpool.Pool
}

func NewQueries(pools *Pools) *Queries {
	return &Queries{writer: pools.Writer, reader: pools.Reader}
}

// ── Albums ────────────────────────────────────────────────────────────────────

func (q *Queries) CreateAlbum(ctx context.Context, albumID string, r model.CreateAlbumRequest) (model.Album, error) {
	var a model.Album
	err := q.writer.QueryRow(ctx,
		`INSERT INTO albums (album_id, title, description, owner)
		 VALUES ($1, $2, $3, $4)
		 RETURNING album_id, title, description, owner, photo_seq, created_at`,
		albumID, r.Title, r.Description, r.Owner,
	).Scan(&a.AlbumID, &a.Title, &a.Description, &a.Owner, &a.PhotoSeq, &a.CreatedAt)
	return a, err
}

// UpsertAlbum inserts or updates an album identified by albumID.
func (q *Queries) UpsertAlbum(ctx context.Context, albumID string, r model.CreateAlbumRequest) (model.Album, error) {
	var a model.Album
	err := q.writer.QueryRow(ctx,
		`INSERT INTO albums (album_id, title, description, owner)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (album_id) DO UPDATE
		   SET title       = EXCLUDED.title,
		       description = EXCLUDED.description,
		       owner       = EXCLUDED.owner
		 RETURNING album_id, title, description, owner, photo_seq, created_at`,
		albumID, r.Title, r.Description, r.Owner,
	).Scan(&a.AlbumID, &a.Title, &a.Description, &a.Owner, &a.PhotoSeq, &a.CreatedAt)
	return a, err
}

// GetAlbum reads from the replica — SELECT only.
func (q *Queries) GetAlbum(ctx context.Context, albumID string) (model.Album, error) {
	var a model.Album
	err := q.reader.QueryRow(ctx,
		`SELECT album_id, title, description, owner, photo_seq, created_at
		 FROM albums WHERE album_id = $1`,
		albumID,
	).Scan(&a.AlbumID, &a.Title, &a.Description, &a.Owner, &a.PhotoSeq, &a.CreatedAt)
	return a, err
}

// ListAlbums returns every album, newest first. No pagination. Reads from replica.
func (q *Queries) ListAlbums(ctx context.Context) ([]model.Album, error) {
	rows, err := q.reader.Query(ctx,
		`SELECT album_id, title, description, owner, photo_seq, created_at
		 FROM albums ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var albums []model.Album
	for rows.Next() {
		var a model.Album
		if err := rows.Scan(&a.AlbumID, &a.Title, &a.Description, &a.Owner, &a.PhotoSeq, &a.CreatedAt); err != nil {
			return nil, err
		}
		albums = append(albums, a)
	}
	return albums, rows.Err()
}

// DeleteAlbum removes an album row. Call DeletePhotosForAlbum first.
func (q *Queries) DeleteAlbum(ctx context.Context, albumID string) error {
	_, err := q.writer.Exec(ctx, `DELETE FROM albums WHERE album_id = $1`, albumID)
	return err
}

// ── Photos ────────────────────────────────────────────────────────────────────

// NextPhotoSeq atomically increments photo_seq and returns the new value.
func (q *Queries) NextPhotoSeq(ctx context.Context, albumID string) (int, error) {
	var seq int
	err := q.writer.QueryRow(ctx,
		`UPDATE albums SET photo_seq = photo_seq + 1
		 WHERE album_id = $1
		 RETURNING photo_seq`,
		albumID,
	).Scan(&seq)
	return seq, err
}

// CreatePhoto inserts a photo with status='processing'.
// s3Key is the FINAL canonical key (albums/…), stored for later S3 cleanup.
func (q *Queries) CreatePhoto(ctx context.Context, photoID, albumID, s3Key string, seq int) (model.Photo, error) {
	var p model.Photo
	err := q.writer.QueryRow(ctx,
		`INSERT INTO photos (photo_id, album_id, seq, s3_key, status)
		 VALUES ($1, $2, $3, $4, 'processing')
		 RETURNING photo_id, album_id, seq, status, url, created_at`,
		photoID, albumID, seq, s3Key,
	).Scan(&p.PhotoID, &p.AlbumID, &p.Seq, &p.Status, &p.URL, &p.CreatedAt)
	return p, err
}

// GetPhoto fetches a single photo by ID. Reads from replica.
func (q *Queries) GetPhoto(ctx context.Context, photoID string) (model.Photo, error) {
	var p model.Photo
	err := q.reader.QueryRow(ctx,
		`SELECT photo_id, album_id, seq, status, url, created_at
		 FROM photos WHERE photo_id = $1`,
		photoID,
	).Scan(&p.PhotoID, &p.AlbumID, &p.Seq, &p.Status, &p.URL, &p.CreatedAt)
	return p, err
}

// ListPhotos reads from the replica.
func (q *Queries) ListPhotos(ctx context.Context, albumID string) ([]model.Photo, error) {
	rows, err := q.reader.Query(ctx,
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

// MarkPhotoProcessed sets status='completed' and the public S3 URL.
func (q *Queries) MarkPhotoProcessed(ctx context.Context, photoID, url string) error {
	_, err := q.writer.Exec(ctx,
		`UPDATE photos SET status = 'completed', url = $2 WHERE photo_id = $1`,
		photoID, url,
	)
	return err
}

// ListPhotoS3Keys returns the s3_key for every photo in an album.
// Used to clean up S3 before deleting an album. Reads from replica.
func (q *Queries) ListPhotoS3Keys(ctx context.Context, albumID string) ([]string, error) {
	rows, err := q.reader.Query(ctx,
		`SELECT s3_key FROM photos WHERE album_id = $1 AND s3_key != ''`,
		albumID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// DeletePhotosForAlbum removes all photo rows for an album.
func (q *Queries) DeletePhotosForAlbum(ctx context.Context, albumID string) error {
	_, err := q.writer.Exec(ctx, `DELETE FROM photos WHERE album_id = $1`, albumID)
	return err
}
