package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pools holds separate connection pools for the primary (writer) and
// read replica (reader). If DATABASE_READER_URL is unset, reader falls
// back to the primary so the app works without a replica.
type Pools struct {
	Writer *pgxpool.Pool
	Reader *pgxpool.Pool
}

func (p *Pools) Close() {
	p.Writer.Close()
	if p.Reader != p.Writer {
		p.Reader.Close()
	}
}

// Connect creates both pools from environment variables.
// MaxConns is kept at 10 per pool per instance because PgBouncer
// (Change 5) multiplexes many app connections through a smaller RDS pool.
// SimpleProtocol is mandatory: PgBouncer in transaction mode does not
// support the extended query protocol used by pgx for prepared statements.
func Connect(ctx context.Context) (*Pools, error) {
	writerCfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, fmt.Errorf("parse writer url: %w", err)
	}
	writerCfg.MaxConns = 10
	writerCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	writer, err := pgxpool.NewWithConfig(ctx, writerCfg)
	if err != nil {
		return nil, fmt.Errorf("writer pool: %w", err)
	}
	if err := writer.Ping(ctx); err != nil {
		return nil, fmt.Errorf("writer ping: %w", err)
	}

	readerURL := os.Getenv("DATABASE_READER_URL")
	if readerURL == "" {
		readerURL = os.Getenv("DATABASE_URL")
	}

	readerCfg, err := pgxpool.ParseConfig(readerURL)
	if err != nil {
		return nil, fmt.Errorf("parse reader url: %w", err)
	}
	readerCfg.MaxConns = 10
	readerCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	reader, err := pgxpool.NewWithConfig(ctx, readerCfg)
	if err != nil {
		return nil, fmt.Errorf("reader pool: %w", err)
	}
	if err := reader.Ping(ctx); err != nil {
		return nil, fmt.Errorf("reader ping: %w", err)
	}

	return &Pools{Writer: writer, Reader: reader}, nil
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
