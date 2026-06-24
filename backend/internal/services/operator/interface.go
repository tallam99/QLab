// Package operator is the staging/local-only operator service: provision demo lab
// workspaces, mint tokens to act as seeded users, and inspect/tear down workspaces
// (decision 0008). It is the domain layer behind the qlab.dev.v1 Connect service;
// the transport adapter lives in internal/devapi. It depends on a store.OperatorStore
// (an elevated, cross-tenant persistence view) and a Minter, both through interfaces.
//
// This package and everything that wires it are mounted only outside production —
// the operator capability does not exist in the production binary at all.
package operator

import (
	"context"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/store"
)

// Minter issues a usable Firebase ID token to act as the user with the given email
// (the "act as anyone" primitive). Implemented by clients/firebase.Minter.
type Minter interface {
	MintToken(ctx context.Context, email string) (string, error)
}

// Service is the operator API's domain logic.
type Service interface {
	// ProvisionLab creates a fresh demo workspace (lab + head/members + pool/resources).
	ProvisionLab(ctx context.Context, spec store.ProvisionSpec) (store.LabWorkspace, error)
	// MintToken issues an ID token to act as the given provisioned user, returning the
	// token and the user. store.ErrNotFound if the user does not exist.
	MintToken(ctx context.Context, userID uuid.UUID) (string, store.User, error)
	// ListLabs lists workspaces, optionally filtered by feature (name substring).
	ListLabs(ctx context.Context, feature string) ([]store.LabSummary, error)
	// GetLab returns a workspace's full state. store.ErrNotFound if absent.
	GetLab(ctx context.Context, labID uuid.UUID) (store.LabState, error)
	// TeardownLab deletes a workspace. store.ErrNotFound if absent.
	TeardownLab(ctx context.Context, labID uuid.UUID) error
}
