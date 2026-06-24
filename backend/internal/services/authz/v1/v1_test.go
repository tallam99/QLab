//go:build testunit

// These tests pin the membership policy the API gates every RPC on: a member passes,
// a non-member is rejected with the sentinel the transport maps to PermissionDenied,
// and a store failure is wrapped (not swallowed as "not a member", which would mask
// an outage as an authz denial).
package v1

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/services/authz"
	"github.com/tallam99/qlab/backend/internal/store"
)

// membershipStore is a narrow fake: it embeds store.Store (so it satisfies the
// interface) but implements only IsMember, the one method the authorizer calls. Any
// other call would nil-panic, which is the point — it must not reach for anything else.
type membershipStore struct {
	store.Store
	ok  bool
	err error
}

func (m membershipStore) IsMember(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return m.ok, m.err
}

func TestRequireMember(t *testing.T) {
	boom := errors.New("db down")
	tests := []struct {
		name      string
		store     membershipStore
		wantErrIs error // nil = expect no error; sentinel to errors.Is against otherwise
	}{
		{"member passes", membershipStore{ok: true}, nil},
		{"non-member rejected", membershipStore{ok: false}, authz.ErrNotMember},
		{"store error wrapped", membershipStore{err: boom}, boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := New(tt.store).RequireMember(context.Background(), uuid.New(), uuid.New())
			if tt.wantErrIs == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErrIs)
		})
	}
}
