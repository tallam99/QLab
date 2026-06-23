// Package pgstore is the Postgres-backed implementation of store.Store. The static
// queries are compiled by sqlc into the sqlcgen subpackage (see sqlc.yaml /
// queries.sql); this package adds the transaction control (lab scoping, the pool
// advisory lock, the per-event unit of work) and converts sqlc rows to the store
// domain types.
package pgstore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tallam99/qlab/backend/internal/store"
	"github.com/tallam99/qlab/backend/internal/store/pgstore/sqlcgen"
)

// Store implements store.Store over a pgx connection pool and the sqlc queries.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
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
	return &Store{pool: pool, q: sqlcgen.New(pool)}, nil
}

// Close releases the store's resources (the connection pool). It satisfies
// io.Closer so the server can track and close it during shutdown.
func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

// CountLabs returns the number of labs — a trivial query proving the store reaches
// the database and reads.
func (s *Store) CountLabs(ctx context.Context) (int, error) {
	n, err := s.q.CountLabs(ctx)
	if err != nil {
		return 0, fmt.Errorf("count labs: %w", err)
	}
	return int(n), nil
}

// --- nullable <-> domain helpers (uuid.Nil / zero time mean SQL NULL) ---

func nilUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func nilTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func derefUUID(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}

func derefTime(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return *p
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
