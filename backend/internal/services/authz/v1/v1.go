// Package v1 is the first implementation of authz.Authorizer, reading membership
// from the application store.
package v1

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/services/authz"
	"github.com/tallam99/qlab/backend/internal/store"
)

// Authorizer implements authz.Authorizer over store.Store.
type Authorizer struct {
	store store.Store
}

// Compile-time guarantee of interface satisfaction.
var _ authz.Authorizer = (*Authorizer)(nil)

// New returns an Authorizer backed by the given store.
func New(s store.Store) *Authorizer {
	return &Authorizer{store: s}
}

// RequireMember returns nil if the user belongs to the lab, authz.ErrNotMember if
// not, or a wrapped error if the membership lookup fails.
func (a *Authorizer) RequireMember(ctx context.Context, userID, labID uuid.UUID) error {
	ok, err := a.store.IsMember(ctx, labID, userID)
	if err != nil {
		return fmt.Errorf("authz: check membership: %w", err)
	}
	if !ok {
		return authz.ErrNotMember
	}
	return nil
}
