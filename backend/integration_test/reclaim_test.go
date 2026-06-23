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

// overrunSetup arranges one resource with an ACTIVE slot owned by Member1 that is
// overrunning at the current (advanced) clock, plus a queued slot owned by Member2
// (the next-in-line). It returns the two slot ids.
func overrunSetup(t *testing.T, lab labFixture) (activeID, waitingID string) {
	t.Helper()
	ctx := context.Background()
	m1 := h.client(lab.Member1, lab.LabID)
	m2 := h.client(lab.Member2, lab.LabID)

	respA, err := m1.CreateSlot(ctx, createReq(lab.PoolID, at(0), 0, 30, "A"))
	require.NoError(t, err)
	activeID = slotIDByNote(t, respA.Msg.GetResult(), "A")
	_, err = m1.ClockIn(ctx, connect.NewRequest(&v1.ClockInRequest{SlotId: activeID}))
	require.NoError(t, err)

	respB, err := m2.CreateSlot(ctx, createReq(lab.PoolID, at(30), 0, 30, "B"))
	require.NoError(t, err)
	waitingID = slotIDByNote(t, respB.Msg.GetResult(), "B")
	return activeID, waitingID
}

// TestForceClockOutOverrun: the next-in-line user boots an overrunning occupant,
// settling it COMPLETE and freeing the resource; a non-next-in-line caller and a
// not-yet-overrunning target are both rejected.
func (s *IntegrationSuite) TestForceClockOutOverrun() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 1)
	activeID, _ := overrunSetup(t, lab)

	// Not overrunning yet (clock still at 0): rejected.
	_, err := h.client(lab.Member2, lab.LabID).
		ForceClockOut(ctx, connect.NewRequest(&v1.ForceClockOutRequest{SlotId: activeID}))
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

	// Overrun: A's scheduled end was 30; advance past it.
	h.clock.set(at(45))

	// The head is not the next-in-line user → denied.
	_, err = h.client(lab.Head, lab.LabID).
		ForceClockOut(ctx, connect.NewRequest(&v1.ForceClockOutRequest{SlotId: activeID}))
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	// The next-in-line user (Member2) boots the occupant.
	_, err = h.client(lab.Member2, lab.LabID).
		ForceClockOut(ctx, connect.NewRequest(&v1.ForceClockOutRequest{SlotId: activeID}))
	require.NoError(t, err)
	assert.Equal(t, "COMPLETE", h.slot(t, activeID).Status)
}

// TestPoke: the next-in-line user nudges an overrunning occupant; it enqueues an
// outbox row and changes no schedule state. A non-overrunning target and a
// non-next-in-line caller are rejected.
func (s *IntegrationSuite) TestPoke() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 1)
	activeID, _ := overrunSetup(t, lab)

	// Not overrunning yet: rejected, nothing enqueued.
	err := pokeErr(ctx, lab, lab.Member2, activeID)
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

	h.clock.set(at(45))

	// Non-next-in-line caller rejected.
	err = pokeErr(ctx, lab, lab.Head, activeID)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	// Next-in-line pokes successfully; one outbox row, occupant still ACTIVE.
	require.NoError(t, pokeErr(ctx, lab, lab.Member2, activeID))
	assert.Equal(t, "ACTIVE", h.slot(t, activeID).Status)
	assert.Equal(t, 1, h.outboxCount(t, lab.LabID, "poke"))
}

func pokeErr(ctx context.Context, lab labFixture, user, slotID string) error {
	_, err := h.client(user, lab.LabID).
		PokeOccupant(ctx, connect.NewRequest(&v1.PokeOccupantRequest{SlotId: slotID}))
	return err
}

// TestForceNoShowGraceLapsed: a slot whose clock-in grace has lapsed is reclaimed
// by the next-in-line user (settled NO_SHOW), and the slot behind pulls forward.
// Before the grace lapses the reclaim is rejected.
func (s *IntegrationSuite) TestForceNoShowGraceLapsed() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 1)
	m1 := h.client(lab.Member1, lab.LabID)
	m2 := h.client(lab.Member2, lab.LabID)

	// B (prio 1) is committed to start at 0 but never clocks in; C (prio 2) waits.
	respB, err := m1.CreateSlot(ctx, createReq(lab.PoolID, at(0), 0, 60, "B"))
	require.NoError(t, err)
	slotB := slotIDByNote(t, respB.Msg.GetResult(), "B")
	respC, err := m2.CreateSlot(ctx, createReq(lab.PoolID, at(60), 60, 30, "C"))
	require.NoError(t, err)
	slotC := slotIDByNote(t, respC.Msg.GetResult(), "C")

	// Grace (15) has not lapsed at t=0: reclaim rejected.
	_, err = m2.ForceNoShow(ctx, connect.NewRequest(&v1.ForceNoShowRequest{SlotId: slotB}))
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

	// Past committed(0) + grace(15): B is reclaimable.
	h.clock.set(at(20))

	// A non-next-in-line caller (the head) is still rejected.
	_, err = h.client(lab.Head, lab.LabID).
		ForceNoShow(ctx, connect.NewRequest(&v1.ForceNoShowRequest{SlotId: slotB}))
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	// The next-in-line user reclaims; B is NO_SHOW and C pulls forward to now.
	_, err = m2.ForceNoShow(ctx, connect.NewRequest(&v1.ForceNoShowRequest{SlotId: slotB}))
	require.NoError(t, err)
	assert.Equal(t, "NO_SHOW", h.slot(t, slotB).Status)
	assert.True(t, h.slot(t, slotC).Actual.Equal(at(20)), "C pulled forward into the reclaimed place")
}
