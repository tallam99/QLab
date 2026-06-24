// Package v1 is the first implementation of authentication.Service: an
// auth.TokenVerifier composed with store.Store. It verifies the token, then maps
// the Firebase identity to a local user, provisioning by email on first login.
package v1

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tallam99/qlab/backend/internal/auth"
	"github.com/tallam99/qlab/backend/internal/services/authentication"
	"github.com/tallam99/qlab/backend/internal/store"
)

// Service implements authentication.Service.
type Service struct {
	verifier auth.TokenVerifier
	store    store.AuthStore
}

// Compile-time guarantee of interface satisfaction.
var _ authentication.Service = (*Service)(nil)

// Options groups the dependencies (a struct so the constructor signature is stable
// as dependencies grow — the codebase convention).
type Options struct {
	Verifier auth.TokenVerifier
	Store    store.AuthStore
}

// New returns a Service. It panics if a required dependency is missing — a wiring
// bug should fail loudly at startup, not at the first request.
func New(opts Options) *Service {
	if opts.Verifier == nil {
		panic("authentication: New requires a Verifier")
	}
	if opts.Store == nil {
		panic("authentication: New requires a Store")
	}
	return &Service{verifier: opts.Verifier, store: opts.Store}
}

// Authenticate resolves rawToken to the local user, provisioning on first login.
func (s *Service) Authenticate(ctx context.Context, rawToken string) (store.User, error) {
	identity, err := s.verifier.Verify(ctx, rawToken)
	if err != nil {
		// Any verification failure is unauthenticated; keep the cause for logs.
		return store.User{}, fmt.Errorf("%w: %w", authentication.ErrUnauthenticated, err)
	}

	// Fast path: the identity is already linked to a user.
	user, err := s.store.UserByFirebaseUID(ctx, identity.FirebaseUID)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.User{}, fmt.Errorf("authentication: lookup by firebase uid: %w", err)
	}

	// First login: link the verified identity to the invited user row (matched by
	// the verified email). No matching invite => not a user of this application.
	return s.provision(ctx, identity)
}

// provision links a verified identity to its invited user row, or reports
// ErrNotProvisioned if no invite exists.
func (s *Service) provision(ctx context.Context, identity auth.Identity) (store.User, error) {
	if identity.Email == "" {
		// Without a verified email there is no invite to match — treat as no invite.
		return store.User{}, authentication.ErrNotProvisioned
	}
	invited, err := s.store.UserByEmail(ctx, identity.Email)
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, authentication.ErrNotProvisioned
	}
	if err != nil {
		return store.User{}, fmt.Errorf("authentication: lookup by email: %w", err)
	}
	// The invited row should be unlinked (firebase_uid empty). If it is already
	// linked to a DIFFERENT identity, the same email maps to two Firebase accounts —
	// a data conflict we refuse rather than silently rebind.
	if invited.FirebaseUID != "" && invited.FirebaseUID != identity.FirebaseUID {
		return store.User{}, fmt.Errorf("authentication: email %q already linked to a different identity", identity.Email)
	}

	first, last := splitName(identity.Name)
	user, err := s.store.LinkFirebaseUID(ctx, invited.ID, identity.FirebaseUID, first, last)
	if err != nil {
		return store.User{}, fmt.Errorf("authentication: link identity: %w", err)
	}
	return user, nil
}

// splitName splits a provider display name into first/last on the first space.
// Google sign-in supplies a single "name" claim; the store keeps name parts, so we
// approximate. Empty parts leave the existing row value untouched (LinkFirebaseUID).
func splitName(name string) (first, last string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	if before, after, found := strings.Cut(name, " "); found {
		return strings.TrimSpace(before), strings.TrimSpace(after)
	}
	return name, ""
}
