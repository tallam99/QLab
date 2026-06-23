//go:build integration

package integrationtest

import (
	"context"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
)

// TestCreateAndList covers booking a slot and reading it back: the slot is placed
// at its earliest feasible start and re-committed (first placement), and ListSlots
// returns it scoped to the lab.
func (s *IntegrationSuite) TestCreateAndList() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 1)
	c := h.client(lab.Member1, lab.LabID)

	// now = base (at 0); a slot desired an hour out with no earliness stays at desired.
	resp, err := c.CreateSlot(ctx, createReq(lab.PoolID, at(60), 0, 60, "first"))
	require.NoError(t, err)
	slotID := slotIDByNote(t, resp.Msg.GetResult(), "first")

	pos := positionFor(t, resp.Msg.GetResult(), slotID)
	assert.True(t, pos.GetActualStart().AsTime().Equal(at(60)), "placed at desired start")
	assert.Equal(t, lab.Res[0], pos.GetAssignedResourceId())
	assert.True(t, pos.GetRecommitted(), "first placement re-commits")

	row := h.slot(t, slotID)
	assert.Equal(t, "SCHEDULED", row.Status)
	assert.Equal(t, lab.Res[0], row.Resource)
	assert.True(t, row.Actual.Equal(at(60)))
	assert.True(t, row.Committed.Equal(at(60)), "committed start persisted")

	list, err := c.ListSlots(ctx, connect.NewRequest(&v1.ListSlotsRequest{ResourcePoolId: lab.PoolID}))
	require.NoError(t, err)
	require.Len(t, list.Msg.GetSlots(), 1)
	assert.Equal(t, slotID, list.Msg.GetSlots()[0].GetId())
}

// TestCreatePullsEarlierWithinLookahead checks that lookahead lets a slot be placed
// before its desired start when capacity is free: desired at 60, lookahead 60, an
// empty pool, now at 0 → placed at its floor (at 0), an hour early.
func (s *IntegrationSuite) TestCreatePullsEarlierWithinLookahead() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 1)
	c := h.client(lab.Member1, lab.LabID)

	resp, err := c.CreateSlot(ctx, createReq(lab.PoolID, at(60), 60, 60, "flex"))
	require.NoError(t, err)
	slotID := slotIDByNote(t, resp.Msg.GetResult(), "flex")
	assert.True(t, h.slot(t, slotID).Actual.Equal(at(0)), "pulled to the earliness floor")
}

// TestClockInClockOutPullsForward is the core lifecycle chain: two queued slots on
// one resource, the first clocks in and then finishes early, and the second is
// pulled forward into the freed time (down to its earliness floor).
func (s *IntegrationSuite) TestClockInClockOutPullsForward() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 1)
	m1 := h.client(lab.Member1, lab.LabID)
	m2 := h.client(lab.Member2, lab.LabID)

	// A (prio 1): desired now, 60 min. B (prio 2): desired at 60, lookahead 30 → floor 30.
	respA, err := m1.CreateSlot(ctx, createReq(lab.PoolID, at(0), 0, 60, "A"))
	require.NoError(t, err)
	slotA := slotIDByNote(t, respA.Msg.GetResult(), "A")
	respB, err := m2.CreateSlot(ctx, createReq(lab.PoolID, at(60), 30, 60, "B"))
	require.NoError(t, err)
	slotB := slotIDByNote(t, respB.Msg.GetResult(), "B")

	// B queues behind A on the single resource.
	assert.True(t, h.slot(t, slotB).Actual.Equal(at(60)), "B initially behind A")

	// A clocks in at its start.
	_, err = m1.ClockIn(ctx, connect.NewRequest(&v1.ClockInRequest{SlotId: slotA}))
	require.NoError(t, err)
	assert.Equal(t, "ACTIVE", h.slot(t, slotA).Status)

	// A finishes early at 30; B pulls forward to its floor (30).
	h.clock.set(at(30))
	resp, err := m1.ClockOut(ctx, connect.NewRequest(&v1.ClockOutRequest{SlotId: slotA}))
	require.NoError(t, err)
	assert.Equal(t, "COMPLETE", h.slot(t, slotA).Status)

	rowB := h.slot(t, slotB)
	assert.True(t, rowB.Actual.Equal(at(30)), "B pulled forward into the freed time")
	assert.True(t, positionFor(t, resp.Msg.GetResult(), slotB).GetRecommitted(), "B re-committed to the earlier start")
}

// TestCancelPullsForward checks that cancelling a queued slot frees its place and
// the slot behind pulls forward.
func (s *IntegrationSuite) TestCancelPullsForward() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 1)
	m1 := h.client(lab.Member1, lab.LabID)
	m2 := h.client(lab.Member2, lab.LabID)

	respA, err := m1.CreateSlot(ctx, createReq(lab.PoolID, at(0), 0, 60, "A"))
	require.NoError(t, err)
	slotA := slotIDByNote(t, respA.Msg.GetResult(), "A")
	respB, err := m2.CreateSlot(ctx, createReq(lab.PoolID, at(60), 60, 60, "B"))
	require.NoError(t, err)
	slotB := slotIDByNote(t, respB.Msg.GetResult(), "B")
	assert.True(t, h.slot(t, slotB).Actual.Equal(at(60)), "B behind A")

	_, err = m1.CancelSlot(ctx, connect.NewRequest(&v1.CancelSlotRequest{SlotId: slotA}))
	require.NoError(t, err)
	assert.Equal(t, "CANCELLED", h.slot(t, slotA).Status)
	assert.True(t, h.slot(t, slotB).Actual.Equal(at(0)), "B pulled forward to its floor after the cancel")
}

// TestClockInOnlyOwnSlot checks the owner guard: a member cannot clock in another
// member's slot.
func (s *IntegrationSuite) TestClockInOnlyOwnSlot() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 1)
	respA, err := h.client(lab.Member1, lab.LabID).
		CreateSlot(ctx, createReq(lab.PoolID, at(0), 0, 60, "A"))
	require.NoError(t, err)
	slotA := slotIDByNote(t, respA.Msg.GetResult(), "A")

	_, err = h.client(lab.Member2, lab.LabID).
		ClockIn(ctx, connect.NewRequest(&v1.ClockInRequest{SlotId: slotA}))
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}
