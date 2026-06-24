// Package v1 is the first implementation of operator.Service over a
// store.OperatorStore (elevated persistence) and an operator.Minter.
package v1

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/services/operator"
	"github.com/tallam99/qlab/backend/internal/store"
)

// Service implements operator.Service.
type Service struct {
	store  store.OperatorStore
	minter operator.Minter
}

// Compile-time guarantee of interface satisfaction.
var _ operator.Service = (*Service)(nil)

// Options groups the dependencies (a struct so the constructor is stable as deps grow).
type Options struct {
	Store  store.OperatorStore
	Minter operator.Minter
}

// New returns a Service. It panics if a required dependency is missing — a wiring
// bug should fail loudly at startup.
func New(opts Options) *Service {
	if opts.Store == nil {
		panic("operator: New requires a Store")
	}
	if opts.Minter == nil {
		panic("operator: New requires a Minter")
	}
	return &Service{store: opts.Store, minter: opts.Minter}
}

// ProvisionLab creates a fresh demo workspace.
func (s *Service) ProvisionLab(ctx context.Context, spec store.ProvisionSpec) (store.LabWorkspace, error) {
	ws, err := s.store.CreateLabWorkspace(ctx, spec)
	if err != nil {
		return store.LabWorkspace{}, fmt.Errorf("operator: provision lab: %w", err)
	}
	return ws, nil
}

// MintToken resolves the user, then mints an ID token for their email.
func (s *Service) MintToken(ctx context.Context, userID uuid.UUID) (string, store.User, error) {
	user, err := s.store.UserByID(ctx, userID)
	if err != nil {
		// store.ErrNotFound passes through for the transport to map to NotFound.
		return "", store.User{}, err
	}
	token, err := s.minter.MintToken(ctx, user.Email)
	if err != nil {
		return "", store.User{}, fmt.Errorf("operator: mint token: %w", err)
	}
	return token, user, nil
}

// ListLabs lists workspaces.
func (s *Service) ListLabs(ctx context.Context, feature string) ([]store.LabSummary, error) {
	labs, err := s.store.ListLabs(ctx, feature)
	if err != nil {
		return nil, fmt.Errorf("operator: list labs: %w", err)
	}
	return labs, nil
}

// GetLab returns a workspace's full state.
func (s *Service) GetLab(ctx context.Context, labID uuid.UUID) (store.LabState, error) {
	state, err := s.store.GetLabState(ctx, labID)
	if err != nil {
		return store.LabState{}, err // store.ErrNotFound passes through
	}
	return state, nil
}

// TeardownLab deletes a workspace.
func (s *Service) TeardownLab(ctx context.Context, labID uuid.UUID) error {
	return s.store.DeleteLab(ctx, labID) // store.ErrNotFound passes through
}
