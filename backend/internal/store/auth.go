package store

import (
	"context"

	"github.com/google/uuid"
)

// AuthStore is the persistence the authentication service needs to resolve a
// verified token to a local user and provision invited users on first login. It is
// a SEPARATE interface from Store (interface segregation): identity resolution is a
// distinct, global (non-lab-scoped) concern from the per-request scheduling surface.
// The same *pgstore.Store satisfies both.
type AuthStore interface {
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
}
