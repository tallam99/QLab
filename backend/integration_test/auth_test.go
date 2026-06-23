//go:build integration

package integrationtest

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
)

// TestUnauthenticated: a request with no principal headers is rejected before it
// reaches any business logic.
func (s *IntegrationSuite) TestUnauthenticated() {
	t := s.T()
	lab := h.makeLab(t, 1)
	_, err := h.anonClient().
		ListSlots(context.Background(), connect.NewRequest(&v1.ListSlotsRequest{ResourcePoolId: lab.PoolID}))
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

// TestNonMemberDenied: an authenticated user who is not a member of the lab they
// claim is denied (RLS checks the lab, but membership is the app-layer gate).
func (s *IntegrationSuite) TestNonMemberDenied() {
	t := s.T()
	lab := h.makeLab(t, 1)
	stranger := uuid.NewString() // a valid uuid, but not a member of the lab
	_, err := h.client(stranger, lab.LabID).
		ListSlots(context.Background(), connect.NewRequest(&v1.ListSlotsRequest{ResourcePoolId: lab.PoolID}))
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

// TestCrossLabIsolation: a member of one lab cannot reach another lab's pool —
// neither by naming it under their own lab (not found), nor by claiming the other
// lab they don't belong to (permission denied).
func (s *IntegrationSuite) TestCrossLabIsolation() {
	t := s.T()
	ctx := context.Background()
	labA := h.makeLab(t, 1)
	labB := h.makeLab(t, 1)

	// Member of A, acting in A, naming B's pool: the pool isn't in their lab.
	_, err := h.client(labA.Member1, labA.LabID).
		ListSlots(ctx, connect.NewRequest(&v1.ListSlotsRequest{ResourcePoolId: labB.PoolID}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// Member of A claiming lab B (which they don't belong to): denied.
	_, err = h.client(labA.Member1, labB.LabID).
		ListSlots(ctx, connect.NewRequest(&v1.ListSlotsRequest{ResourcePoolId: labB.PoolID}))
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

// TestCreateInvalidArgument: a malformed booking (zero duration) is rejected.
func (s *IntegrationSuite) TestCreateInvalidArgument() {
	t := s.T()
	lab := h.makeLab(t, 1)
	_, err := h.client(lab.Member1, lab.LabID).
		CreateSlot(context.Background(), connect.NewRequest(&v1.CreateSlotRequest{
			ResourcePoolId:  lab.PoolID,
			DesiredStart:    tspb(at(60)),
			DurationMinutes: 0, // invalid
		}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
