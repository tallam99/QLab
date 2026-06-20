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
	pool *pgxpool.Pool // used by the query methods that land in Phase 4
}

// Compile-time guarantee that *Store satisfies the store interface.
var _ store.Store = (*Store)(nil)

// New verifies the pool is reachable and returns a Store. Pinging at construction
// means a Store handed onward is already health-checked, so recipients can treat
// it as live — which is why health is not part of the store.Store interface.
func New(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Ready reports whether the database is currently reachable. It backs the
// service's readiness probe (ongoing health), distinct from the construction-time
// check in New.
func (s *Store) Ready(ctx context.Context) error {
	return s.pool.Ping(ctx)
}
