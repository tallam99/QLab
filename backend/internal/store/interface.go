// Package store defines the application's data store: the business-level
// persistence the service depends on. The interface lives here so handlers
// depend on behavior, not on pgx; the Postgres-backed implementation is in
// store/pgstore.
//
// Health is intentionally NOT part of this interface. A Store is verified when
// it's constructed (see pgstore.New), so any recipient can treat the Store it's
// handed as live without re-checking. Ongoing readiness is a separate concern
// (see pgstore.Store.Ready, wired to the server's readiness probe).
package store

// Store is the data store the service reads and writes its data model through. It
// is intentionally minimal today; data methods land with the schema in Phase 4.
type Store interface{}
