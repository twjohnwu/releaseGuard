package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Pool struct {
	*pgxpool.Pool
}

// NewPool returns a pgxpool that is PgBouncer-transaction-pool-mode safe:
// - simple query protocol (no prepared statements)
// - max 25 conns
func NewPool(ctx context.Context, url string) (*Pool, error) {
	if url == "" {
		return nil, fmt.Errorf("postgres url is empty")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	cfg.MaxConns = 25
	cfg.MinConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new pool: %w", err)
	}
	return &Pool{Pool: pool}, nil
}
