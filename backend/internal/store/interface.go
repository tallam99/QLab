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

// Store is the data store the service reads and writes its data model through. It
// is intentionally empty today; the business-domain methods land with the schema
// in Phase 4.
type Store interface{}
