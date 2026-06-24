// Package auth verifies bearer credentials and reports who presented them. It is
// the transport-agnostic identity seam: the middleware hands it a raw token and
// gets back a provider-neutral Identity, never touching the provider SDK directly.
// The interface lives here so the rest of the service depends on behavior, not on
// Firebase; the Firebase-backed implementation is in auth/firebaseauth (mirroring
// store/pgstore). A fake implementation backs unit tests.
//
// This answers only "who is this token?" — it does NOT decide what the caller may
// do (that is authz) nor resolve the token to a local users row (that is the
// authentication service). Keeping verification this thin is what lets the local
// Auth emulator and real Firebase share one code path.
package auth

import (
	"context"
	"errors"
)

// ErrInvalidToken is returned when a token is missing, malformed, expired, or
// fails signature/audience verification. The transport maps it to 401
// Unauthenticated. It deliberately does not distinguish the reason: leaking
// "expired" vs "bad signature" to an unauthenticated caller has no upside.
var ErrInvalidToken = errors.New("auth: invalid token")

// Identity is the verified, provider-neutral claims the service cares about. The
// FirebaseUID is the stable external identity (mapped to a local users row by the
// authentication service); Email/Name seed first-login provisioning.
type Identity struct {
	// FirebaseUID is the provider's stable user id (the token subject).
	FirebaseUID string
	// Email is the email claim, lowercased by the provider/our verifier so it
	// matches the canonical lowercase users.email. Trust it for invite matching only
	// when EmailVerified is true.
	Email string
	// EmailVerified reports whether the provider has confirmed the user owns Email.
	// First-login provisioning matches an invite by email, so an unverified email
	// must not be trusted to claim someone else's invited row.
	EmailVerified bool
	// Name is the display name claim, if the provider supplied one (may be empty).
	Name string
}

// TokenVerifier verifies a raw bearer token and returns its identity. It returns
// ErrInvalidToken for any verification failure (so callers need not inspect
// provider error types) and a wrapped error only for genuine infrastructure faults.
type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (Identity, error)
}
