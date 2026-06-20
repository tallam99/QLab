// Package db owns the Postgres connection pool.
//
// Phase 2 only establishes and verifies connectivity (a pgx pool plus a boot
// ping); the query layer (sqlc/squirrel) and migrations land in Phase 4.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Options configures Connect. A struct (rather than positional params) so the
// pool can take new knobs (max conns, timeouts) without churning call sites.
type Options struct {
	// DatabaseURL is the pgx-format Postgres connection string.
	DatabaseURL string
}

// Connect opens a pgx connection pool and verifies it with a ping, so a bad
// connection string or an unreachable database fails fast at boot rather than on
// the first query. The caller owns the returned pool and must Close it.
func Connect(ctx context.Context, opts Options) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, opts.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
