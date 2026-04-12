package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func New(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("db ping: %w", err)
	}
	return pool, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS albums (
			album_id    TEXT        PRIMARY KEY,
			title       TEXT        NOT NULL,
			description TEXT        NOT NULL DEFAULT '',
			owner       TEXT        NOT NULL,
			photo_seq   INTEGER     NOT NULL DEFAULT 0,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		);

		CREATE TABLE IF NOT EXISTS photos (
			photo_id    TEXT        PRIMARY KEY,
			album_id    TEXT        NOT NULL REFERENCES albums(album_id),
			seq         INTEGER     NOT NULL,
			status      TEXT        NOT NULL DEFAULT 'processing',
			s3_key      TEXT        NOT NULL DEFAULT '',
			url         TEXT,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		);

		CREATE INDEX IF NOT EXISTS photos_album_idx ON photos(album_id);

		-- Idempotent column addition for existing deployments that lack s3_key.
		DO $$
		BEGIN
			ALTER TABLE photos ADD COLUMN IF NOT EXISTS s3_key TEXT NOT NULL DEFAULT '';
		EXCEPTION WHEN duplicate_column THEN NULL;
		END $$;
	`)
	return err
}
