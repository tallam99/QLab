// Package authentication resolves a bearer token to the local user it represents,
// provisioning that user on first login. It is the bridge between auth (which only
// says "this token is valid and belongs to Firebase uid X") and the application's
// users table (which says "uid X is our user Y"). The middleware depends on this
// interface; the v1 implementation composes an auth.TokenVerifier with store.Store.
//
// It deliberately does NOT decide lab membership or roles — that is authz, checked
// downstream once a lab is known. This service answers only "who, in our system, is
// this caller?".
package authentication

import (
	"context"
	"errors"

	"github.com/tallam99/qlab/backend/internal/store"
)

// ErrUnauthenticated means the token was missing, malformed, or failed
// verification. The transport maps it to 401.
var ErrUnauthenticated = errors.New("authentication: unauthenticated")

// ErrNotProvisioned means the token is valid but no user exists for its identity
// and none could be provisioned — the email was never invited (or is unverified, so
// it can't be trusted to match an invite). The caller is authenticated to Firebase
// but is not a user of this application; the transport maps it to 403 (authenticated,
// but not permitted in). Invite-only by design (PLAN Phase 8): there is no
// self-service signup.
var ErrNotProvisioned = errors.New("authentication: user not provisioned (no invite)")

// ErrIdentityConflict means a valid token's email matches an invited user row that
// is already linked to a DIFFERENT Firebase identity — the same address maps to two
// Firebase accounts. We refuse rather than silently rebind; it needs an operator to
// resolve the duplicate, so the transport maps it to FailedPrecondition (not an
// internal error, and not retryable until the data is fixed).
var ErrIdentityConflict = errors.New("authentication: email already linked to a different identity")

// Service resolves a raw bearer token to the local user it authenticates as.
type Service interface {
	// Authenticate verifies rawToken and returns the local user it identifies,
	// linking the Firebase identity to an invited user row on first login. It returns
	// ErrUnauthenticated for an invalid token, ErrNotProvisioned for a valid token
	// with no matching invite, or a wrapped error for infrastructure failures.
	Authenticate(ctx context.Context, rawToken string) (store.User, error)
}
