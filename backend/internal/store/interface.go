// Package store defines the application's data store: the business-domain
// persistence the service depends on. The interface lives here so the service
// depends on behavior, not on pgx; the Postgres-backed implementation is in
// store/pgstore.
//
// A Store is handed to the service already constructed and verified (pgstore.New
// pings on the way out), so it is guaranteed ready — the service neither
// configures it nor health-checks it. The interface therefore holds only
// business-domain operations, nothing infrastructural. Callers choose the
// implementation: the real Postgres store, or a stub in tests.
package store

import "context"

//go:generate go tool mockery

// Store is the data store the service reads and writes its data model through. The
// full set of business-domain methods lands with the API in Phase 7; for now it
// carries a single trivial read that proves the store connects and queries.
type Store interface {
	// CountLabs returns the number of labs. A connectivity/smoke query, not a
	// product operation — the real lab/slot/membership methods arrive in Phase 7.
	CountLabs(ctx context.Context) (int, error)
}
