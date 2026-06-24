//go:build integration

package integrationtest

import (
	"context"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
)

// TestGetScheduleReadOnly covers the read path behind the UI: GetSchedule returns the
// pool's current schedule (slots + placements) without mutating anything. It must be
// idempotent and must not re-commit (a read never notifies or moves committed state).
func (s *IntegrationSuite) TestGetScheduleReadOnly() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 1)
	c := h.client(t, lab.Member1, lab.LabID)

	create, err := c.CreateSlot(ctx, createReq(lab.PoolID, at(60), 0, 60, "first"))
	require.NoError(t, err)
	slotID := slotIDByNote(t, create.Msg.GetResult(), "first")

	// GetSchedule returns the same Result shape, with the slot placed at its start.
	sched, err := c.GetSchedule(ctx, connect.NewRequest(&v1.GetScheduleRequest{ResourcePoolId: lab.PoolID}))
	require.NoError(t, err)
	result := sched.Msg.GetResult()
	require.Len(t, result.GetSlots(), 1)
	assert.Equal(t, slotID, result.GetSlots()[0].GetId())

	pos := positionFor(t, result, slotID)
	assert.True(t, pos.GetActualStart().AsTime().Equal(at(60)), "placed at desired start")
	assert.Equal(t, lab.Res[0], pos.GetAssignedResourceId())
	// The create already committed this start; re-reading the stable schedule must not
	// re-commit it — a read never notifies. (recommitted ⇒ a notification would fire.)
	assert.False(t, pos.GetRecommitted(), "a read never re-commits")

	// Committed start in the row is unchanged by the read — proof it persisted nothing.
	row := h.slot(t, slotID)
	assert.True(t, row.Committed.Equal(at(60)), "read did not move committed start")

	// Idempotent: a second call yields the same placement.
	again, err := c.GetSchedule(ctx, connect.NewRequest(&v1.GetScheduleRequest{ResourcePoolId: lab.PoolID}))
	require.NoError(t, err)
	require.Len(t, again.Msg.GetResult().GetSlots(), 1)
	assert.True(t, positionFor(t, again.Msg.GetResult(), slotID).GetActualStart().AsTime().Equal(at(60)))
}
