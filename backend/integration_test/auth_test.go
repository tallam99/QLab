//go:build integration

package integrationtest

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
)

// TestUnauthenticated: a request with no bearer token is rejected before any
// business logic.
func (s *IntegrationSuite) TestUnauthenticated() {
	t := s.T()
	lab := h.makeLab(t, 1)
	_, err := h.anonClient().
		ListSlots(context.Background(), connect.NewRequest(&v1.ListSlotsRequest{ResourcePoolId: lab.PoolID}))
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

// TestInvalidToken: a malformed bearer token fails verification and is rejected as
// unauthenticated (not as a server error).
func (s *IntegrationSuite) TestInvalidToken() {
	t := s.T()
	lab := h.makeLab(t, 1)
	_, err := h.bearerClient("not-a-real-jwt", lab.LabID).
		ListSlots(context.Background(), connect.NewRequest(&v1.ListSlotsRequest{ResourcePoolId: lab.PoolID}))
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

// TestNotProvisioned: a validly-authenticated user whose email was never invited
// has no users row and cannot be provisioned — distinct from a bad token, this is
// PermissionDenied (authenticated, but not a user of this application).
func (s *IntegrationSuite) TestNotProvisioned() {
	t := s.T()
	lab := h.makeLab(t, 1)
	uninvited := "uninvited-" + lab.LabID + "@example.com"
	_, err := h.clientForEmail(t, uninvited, lab.LabID).
		ListSlots(context.Background(), connect.NewRequest(&v1.ListSlotsRequest{ResourcePoolId: lab.PoolID}))
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

// TestFirstLoginProvisioning: an invited user who has never logged in (firebase_uid
// NULL) is provisioned on their first authenticated call — the uid is linked and
// the call succeeds.
func (s *IntegrationSuite) TestFirstLoginProvisioning() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 1)

	require.Empty(t, h.firebaseUID(t, lab.Member1), "invited user starts unlinked")

	_, err := h.client(t, lab.Member1, lab.LabID).
		ListSlots(ctx, connect.NewRequest(&v1.ListSlotsRequest{ResourcePoolId: lab.PoolID}))
	require.NoError(t, err, "first authenticated call should succeed and provision")

	assert.NotEmpty(t, h.firebaseUID(t, lab.Member1), "first login should link the firebase uid")
}

// TestCrossLabIsolation: a member of one lab cannot reach another lab's pool —
// neither by naming it under their own lab (not found), nor by claiming the other
// lab they don't belong to (permission denied). Membership, not RLS, is the gate.
func (s *IntegrationSuite) TestCrossLabIsolation() {
	t := s.T()
	ctx := context.Background()
	labA := h.makeLab(t, 1)
	labB := h.makeLab(t, 1)

	// Member of A, acting in A, naming B's pool: the pool isn't in their lab.
	_, err := h.client(t, labA.Member1, labA.LabID).
		ListSlots(ctx, connect.NewRequest(&v1.ListSlotsRequest{ResourcePoolId: labB.PoolID}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// Member of A claiming lab B (which they don't belong to): denied.
	_, err = h.client(t, labA.Member1, labB.LabID).
		ListSlots(ctx, connect.NewRequest(&v1.ListSlotsRequest{ResourcePoolId: labB.PoolID}))
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

// TestCreateInvalidArgument: a malformed booking (zero duration) is rejected by the
// validation interceptor, after authentication.
func (s *IntegrationSuite) TestCreateInvalidArgument() {
	t := s.T()
	lab := h.makeLab(t, 1)
	_, err := h.client(t, lab.Member1, lab.LabID).
		CreateSlot(context.Background(), connect.NewRequest(&v1.CreateSlotRequest{
			ResourcePoolId:  lab.PoolID,
			DesiredStart:    tspb(at(60)),
			DurationMinutes: 0, // invalid
		}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// firebaseUID reads a user's linked firebase_uid via the admin pool ("" when NULL).
func (h *harness) firebaseUID(t *testing.T, userID string) string {
	t.Helper()
	var uid *string
	require.NoError(t, h.admin.QueryRow(context.Background(),
		`SELECT firebase_uid FROM users WHERE users_id = $1`, userID).Scan(&uid))
	if uid == nil {
		return ""
	}
	return *uid
}
