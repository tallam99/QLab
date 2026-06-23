//go:build testunit

package v1

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/auth"
	"github.com/tallam99/qlab/backend/internal/services/authentication"
	"github.com/tallam99/qlab/backend/internal/store"
)

// fakeVerifier returns a fixed identity/error, standing in for Firebase.
type fakeVerifier struct {
	identity auth.Identity
	err      error
}

func (f fakeVerifier) Verify(context.Context, string) (auth.Identity, error) {
	return f.identity, f.err
}

// fakeStore implements store.Store with configurable user-lookup behavior; the
// non-user methods are unused here and return zero values.
type fakeStore struct {
	byUID    map[string]store.User
	byEmail  map[string]store.User
	linked   *store.User // captures the LinkFirebaseUID result
	linkErr  error
	uidErr   error // overrides the default not-found for UserByFirebaseUID
	emailErr error // overrides the default not-found for UserByEmail
}

func (s *fakeStore) UserByFirebaseUID(_ context.Context, fbUID string) (store.User, error) {
	if s.uidErr != nil {
		return store.User{}, s.uidErr
	}
	if u, ok := s.byUID[fbUID]; ok {
		return u, nil
	}
	return store.User{}, store.ErrNotFound
}

func (s *fakeStore) UserByEmail(_ context.Context, email string) (store.User, error) {
	if s.emailErr != nil {
		return store.User{}, s.emailErr
	}
	if u, ok := s.byEmail[email]; ok {
		return u, nil
	}
	return store.User{}, store.ErrNotFound
}

func (s *fakeStore) LinkFirebaseUID(_ context.Context, userID uuid.UUID, fbUID, first, last string) (store.User, error) {
	if s.linkErr != nil {
		return store.User{}, s.linkErr
	}
	u := store.User{ID: userID, FirebaseUID: fbUID, FirstName: first, LastName: last}
	s.linked = &u
	return u, nil
}

func (*fakeStore) CountLabs(context.Context) (int, error)                       { return 0, nil }
func (*fakeStore) IsMember(context.Context, uuid.UUID, uuid.UUID) (bool, error) { return false, nil }
func (*fakeStore) ResourcePoolByID(context.Context, uuid.UUID, uuid.UUID) (store.ResourcePool, error) {
	return store.ResourcePool{}, nil
}
func (*fakeStore) SlotByID(context.Context, uuid.UUID, uuid.UUID) (store.Slot, error) {
	return store.Slot{}, nil
}
func (*fakeStore) ListSlots(context.Context, uuid.UUID, uuid.UUID) ([]store.Slot, error) {
	return nil, nil
}
func (*fakeStore) WithPool(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, func(store.PoolState) (store.PoolMutation, error)) error {
	return nil
}

// TestAuthenticate covers the verify→resolve→provision flow: an invalid token, the
// already-linked fast path, first-login provisioning by email, and the rejection
// cases (no invite, email already linked elsewhere).
func TestAuthenticate(t *testing.T) {
	existingID := uuid.New()
	invitedID := uuid.New()

	tests := []struct {
		name        string
		verifier    fakeVerifier
		store       *fakeStore
		wantErr     error // sentinel the result must wrap (nil = success)
		wantUserID  uuid.UUID
		wantLinked  bool   // provisioning should have linked the invited row
		wantFBOnNew string // expected firebase uid on the linked row
	}{
		{
			name:     "invalid token is unauthenticated",
			verifier: fakeVerifier{err: auth.ErrInvalidToken},
			store:    &fakeStore{},
			wantErr:  authentication.ErrUnauthenticated,
		},
		{
			name:     "already linked returns the user without provisioning",
			verifier: fakeVerifier{identity: auth.Identity{FirebaseUID: "fb-1", Email: "a@x.io"}},
			store: &fakeStore{
				byUID: map[string]store.User{"fb-1": {ID: existingID, FirebaseUID: "fb-1", Email: "a@x.io"}},
			},
			wantUserID: existingID,
		},
		{
			name:     "first login provisions the invited row by email",
			verifier: fakeVerifier{identity: auth.Identity{FirebaseUID: "fb-2", Email: "invited@x.io", Name: "Ada Lovelace"}},
			store: &fakeStore{
				byEmail: map[string]store.User{"invited@x.io": {ID: invitedID, Email: "invited@x.io"}},
			},
			wantUserID:  invitedID,
			wantLinked:  true,
			wantFBOnNew: "fb-2",
		},
		{
			name:     "no invite is not provisioned",
			verifier: fakeVerifier{identity: auth.Identity{FirebaseUID: "fb-3", Email: "stranger@x.io"}},
			store:    &fakeStore{},
			wantErr:  authentication.ErrNotProvisioned,
		},
		{
			name:     "verified token with no email is not provisioned",
			verifier: fakeVerifier{identity: auth.Identity{FirebaseUID: "fb-4"}},
			store:    &fakeStore{},
			wantErr:  authentication.ErrNotProvisioned,
		},
		{
			name:     "email linked to a different identity is refused",
			verifier: fakeVerifier{identity: auth.Identity{FirebaseUID: "fb-new", Email: "taken@x.io"}},
			store: &fakeStore{
				byEmail: map[string]store.User{"taken@x.io": {ID: invitedID, FirebaseUID: "fb-old", Email: "taken@x.io"}},
			},
			wantErr: nil, // not a sentinel; just must be a non-nil error (checked below)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := New(Options{Verifier: tt.verifier, Store: tt.store})
			user, err := svc.Authenticate(context.Background(), "token")

			// The conflict case: expect a non-nil error that is none of the sentinels.
			if tt.name == "email linked to a different identity is refused" {
				if err == nil {
					t.Fatal("expected an error for the identity conflict")
				}
				return
			}

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want wrap of %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if user.ID != tt.wantUserID {
				t.Errorf("user.ID = %v, want %v", user.ID, tt.wantUserID)
			}
			if tt.wantLinked {
				if tt.store.linked == nil {
					t.Fatal("expected the invited row to be linked")
				}
				if tt.store.linked.FirebaseUID != tt.wantFBOnNew {
					t.Errorf("linked firebase uid = %q, want %q", tt.store.linked.FirebaseUID, tt.wantFBOnNew)
				}
			}
		})
	}
}

// TestSplitName checks the display-name split feeding first/last name parts.
func TestSplitName(t *testing.T) {
	tests := []struct {
		in          string
		first, last string
	}{
		{"", "", ""},
		{"Ada", "Ada", ""},
		{"Ada Lovelace", "Ada", "Lovelace"},
		{"  Ada  Lovelace  ", "Ada", "Lovelace"},
		{"Ada B Lovelace", "Ada", "B Lovelace"},
	}
	for _, tt := range tests {
		first, last := splitName(tt.in)
		if first != tt.first || last != tt.last {
			t.Errorf("splitName(%q) = (%q, %q), want (%q, %q)", tt.in, first, last, tt.first, tt.last)
		}
	}
}
