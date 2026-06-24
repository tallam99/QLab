//go:build testunit

package v1

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/store"
)

// fakeOperatorStore implements store.OperatorStore with configurable behavior.
type fakeOperatorStore struct {
	user       store.User
	userErr    error
	provided   store.ProvisionSpec
	workspace  store.LabWorkspace
	deleteErr  error
	deletedLab uuid.UUID
}

func (f *fakeOperatorStore) CreateLabWorkspace(_ context.Context, spec store.ProvisionSpec) (store.LabWorkspace, error) {
	f.provided = spec
	return f.workspace, nil
}
func (f *fakeOperatorStore) ListLabs(context.Context, string) ([]store.LabSummary, error) {
	return nil, nil
}
func (f *fakeOperatorStore) GetLabState(context.Context, uuid.UUID) (store.LabState, error) {
	return store.LabState{}, nil
}
func (f *fakeOperatorStore) DeleteLab(_ context.Context, labID uuid.UUID) error {
	f.deletedLab = labID
	return f.deleteErr
}
func (f *fakeOperatorStore) UserByID(context.Context, uuid.UUID) (store.User, error) {
	return f.user, f.userErr
}

// fakeMinter records the email it was asked to mint for.
type fakeMinter struct {
	token string
	err   error
	email string
}

func (f *fakeMinter) MintToken(_ context.Context, email string) (string, error) {
	f.email = email
	return f.token, f.err
}

// TestMintToken resolves the user, mints for their email, and passes a missing user
// through as store.ErrNotFound.
func TestMintToken(t *testing.T) {
	userID := uuid.New()

	t.Run("resolves email and mints", func(t *testing.T) {
		st := &fakeOperatorStore{user: store.User{ID: userID, Email: "x@y.io"}}
		m := &fakeMinter{token: "tok"}
		svc := New(Options{Store: st, Minter: m})

		token, user, err := svc.MintToken(context.Background(), userID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "tok" {
			t.Errorf("token = %q, want tok", token)
		}
		if user.ID != userID {
			t.Errorf("user.ID = %v, want %v", user.ID, userID)
		}
		if m.email != "x@y.io" {
			t.Errorf("minted for %q, want x@y.io", m.email)
		}
	})

	t.Run("missing user is not found", func(t *testing.T) {
		st := &fakeOperatorStore{userErr: store.ErrNotFound}
		svc := New(Options{Store: st, Minter: &fakeMinter{}})
		_, _, err := svc.MintToken(context.Background(), userID)
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("error = %v, want store.ErrNotFound", err)
		}
	})
}

// TestProvisionLab passes the spec straight through to the store.
func TestProvisionLab(t *testing.T) {
	st := &fakeOperatorStore{workspace: store.LabWorkspace{Lab: store.Lab{Name: "search"}}}
	svc := New(Options{Store: st, Minter: &fakeMinter{}})

	spec := store.ProvisionSpec{Feature: "search", MemberCount: 3, ResourceCount: 2}
	ws, err := svc.ProvisionLab(context.Background(), spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.provided != spec {
		t.Errorf("store got spec %+v, want %+v", st.provided, spec)
	}
	if ws.Lab.Name != "search" {
		t.Errorf("workspace lab name = %q, want search", ws.Lab.Name)
	}
}

// TestNewRequiresDependencies: missing deps panic (loud wiring failure).
func TestNewRequiresDependencies(t *testing.T) {
	assertPanics(t, func() { New(Options{Minter: &fakeMinter{}}) })
	assertPanics(t, func() { New(Options{Store: &fakeOperatorStore{}}) })
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected a panic")
		}
	}()
	fn()
}
