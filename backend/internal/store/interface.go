// Package store defines the application's data store: the business-level
// persistence the service depends on. The interface lives here so handlers
// depend on behavior, not on pgx; the Postgres-backed implementation is in
// store/pgstore.
package store

import "context"

// Store is the data store the service reads and writes its data model through.
// It grows with the schema (Phase 4); for now it exposes only Ping, which the
// readiness probe uses to confirm the datastore is reachable.
type Store interface {
	// Ping verifies the datastore is reachable.
	Ping(ctx context.Context) error
}
