// Package store defines the application's data store: the business-level
// persistence the service depends on. The interface lives here so the service
// depends on behavior, not on pgx; the Postgres-backed implementation is in
// store/pgstore.
//
// A Store is handed to the service already constructed and health-checked (see
// pgstore.New) — the service uses it, it doesn't configure it. Callers choose the
// implementation: the real Postgres store, or a no-op/stub in tests.
package store

import "context"

// Store is the data store the service reads and writes its data model through. It
// is intentionally minimal today; data methods land with the schema in Phase 4.
type Store interface {
	// Ready reports whether the store is currently reachable. The readiness probe
	// calls it; a no-op store can simply return nil.
	Ready(ctx context.Context) error
}
