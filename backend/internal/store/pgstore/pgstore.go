// Package pgstore is the Postgres-backed implementation of store.Store.
package pgstore

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tallam99/qlab/backend/internal/store"
)

// Store implements store.Store over a pgx connection pool.
type Store struct {
	pool *pgxpool.Pool // used by Ready and the query methods that land in Phase 4
}

// Compile-time guarantee that *Store satisfies the store interface.
var _ store.Store = (*Store)(nil)

// New verifies the pool is reachable and returns a Store, so the store handed to
// the service is already health-checked at construction. Ongoing readiness is
// reported by Ready.
func New(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Ready reports whether the database is currently reachable — a thin proxy over
// the pool's ping that backs the service's readiness probe.
func (s *Store) Ready(ctx context.Context) error {
	return s.pool.Ping(ctx)
}
