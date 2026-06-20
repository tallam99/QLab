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

// New verifies the pool is reachable and returns a Store, so a returned Store is
// guaranteed ready — callers neither re-check nor health-probe it. An error here
// is a failed dependency initialization: the service won't start.
func New(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the store's resources (the connection pool). It satisfies
// io.Closer so the server can track and close it during shutdown.
func (s *Store) Close() error {
	s.pool.Close()
	return nil
}
