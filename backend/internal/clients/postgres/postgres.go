// Package postgres owns Postgres connection setup — the "client tech", not data
// access. The data model lives behind internal/store; this just hands back a
// ready connection pool. Future external clients (Firebase, object storage) get
// sibling packages under internal/clients.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Options configures New. A struct (rather than positional params) so the pool
// can take new knobs (max conns, timeouts) without churning call sites.
type Options struct {
	// DatabaseURL is the pgx-format Postgres connection string.
	DatabaseURL string
}

// New builds a pgx connection pool. Connection is lazy (pgxpool.New does not dial
// here); verify reachability with the store's Ping before serving. The caller
// owns the returned pool and must Close it.
func New(ctx context.Context, opts Options) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, opts.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	return pool, nil
}
