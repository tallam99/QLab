// Package pgstore is the Postgres-backed implementation of store.Store.
package pgstore

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tallam99/qlab/backend/internal/store"
)

// Store implements store.Store over a pgx connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// Compile-time guarantee that *Store satisfies the store interface.
var _ store.Store = (*Store)(nil)

// New returns a Store backed by pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Ping verifies the database is reachable — a thin proxy over the pool's ping.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}
