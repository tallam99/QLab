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
//
// The store speaks the persistence domain (Slot, Pool, Resource, OutboxRow) and
// is deliberately engine-agnostic: it never imports dynamicqueue. The scheduling
// layer converts these to the engine's types and back at its boundary.
package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

//go:generate go tool mockery

// ErrNotFound is returned by the single-row lookups when no row matches (within
// the caller's lab scope). Callers map it to a not-found / permission error at the
// edge rather than leaking pgx.ErrNoRows.
var ErrNotFound = errors.New("store: not found")

// PoolState is the locked snapshot WithPool hands to its callback: the pool's
// live slots (SCHEDULED + ACTIVE, locked FOR UPDATE) and its resources. History
// is excluded — the engine only ever sees the live world.
type PoolState struct {
	Slots     []Slot
	Resources []Resource
}

// PoolMutation is what a WithPool callback returns for the store to persist
// atomically: the full desired state of every slot to write (insert-or-update by
// ID) and any outbox rows to enqueue.
type PoolMutation struct {
	Slots  []Slot
	Outbox []OutboxRow
}

// Store is the data store the service reads and writes its data model through.
type Store interface {
	// CountLabs returns the number of labs — a connectivity/smoke query retained
	// from the bootstrap, not a product operation.
	CountLabs(ctx context.Context) (int, error)

	// IsMember reports whether userID belongs to labID. Authorization uses it to
	// reject callers acting on a lab they are not a member of.
	IsMember(ctx context.Context, labID, userID uuid.UUID) (bool, error)

	// UserByFirebaseUID loads the user linked to a verified Firebase identity.
	// Returns ErrNotFound when the identity has not been linked yet — the signal to
	// attempt first-login provisioning by email. Users are global (not lab-scoped),
	// so this read is unscoped.
	UserByFirebaseUID(ctx context.Context, firebaseUID string) (User, error)

	// UserByEmail loads the user with the given canonical (lowercase) email. Returns
	// ErrNotFound when no one was invited at that address — an authenticated caller
	// with no matching invite is rejected (no self-provisioning of unknown emails).
	UserByEmail(ctx context.Context, email string) (User, error)

	// LinkFirebaseUID binds a Firebase uid to an existing, not-yet-linked user row
	// (first-login provisioning) and fills any missing name parts from the provider,
	// recording the user as their own updater. It returns the updated user.
	LinkFirebaseUID(ctx context.Context, userID uuid.UUID, firebaseUID, firstName, lastName string) (User, error)

	// ResourcePoolByID loads a pool within labID (resolving its kind). Returns
	// ErrNotFound if no such pool exists in that lab — which is also how a cross-lab
	// pool id is rejected.
	ResourcePoolByID(ctx context.Context, labID, poolID uuid.UUID) (ResourcePool, error)

	// SlotByID loads a single slot within labID, so a slot-targeting RPC can
	// resolve its pool (the lock + authoritative state check happen inside
	// WithPool). Returns ErrNotFound if absent in that lab.
	SlotByID(ctx context.Context, labID, slotID uuid.UUID) (Slot, error)

	// ListSlots returns the pool's slots (full lifecycle) scoped to labID, ordered
	// for display.
	ListSlots(ctx context.Context, labID, poolID uuid.UUID) ([]Slot, error)

	// WithPool runs fn inside one transaction with the lab's RLS scope set
	// (app.current_lab_id), the pool serialized by an advisory lock, and the pool's
	// live slots locked FOR UPDATE — the "one transaction per event" contract
	// (ALGORITHM §10). It hands fn the locked PoolState, then persists the returned
	// PoolMutation (upserting slots by id and enqueuing outbox rows) and commits;
	// any error rolls the whole thing back. actorUserID is recorded as
	// created_by/updated_by on written rows.
	WithPool(ctx context.Context, labID, poolID, actorUserID uuid.UUID, fn func(PoolState) (PoolMutation, error)) error
}
