//go:build integration

package integrationtest

import (
	"context"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
)

// TestMultiResourceFanOutAndReflow is a longer chain across two resources: three
// same-time bookings fan out (two run at once, the third queues), the two front
// slots clock in, and when one finishes early the queued slot is pulled forward
// onto the freed resource. It exercises fan-out, queueing behind ACTIVE occupancy,
// and cross-resource pull-forward in one flow.
func (s *IntegrationSuite) TestMultiResourceFanOutAndReflow() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 2)
	m1 := h.client(t, lab.Member1, lab.LabID)
	m2 := h.client(t, lab.Member2, lab.LabID)
	head := h.client(t, lab.Head, lab.LabID)

	// Three slots all desired at 0, 60 min, no earliness. Two fan across the two
	// resources; the third queues behind one of them.
	respA, err := m1.CreateSlot(ctx, createReq(lab.PoolID, at(0), 0, 60, "A"))
	require.NoError(t, err)
	slotA := slotIDByNote(t, respA.Msg.GetResult(), "A")
	respB, err := m2.CreateSlot(ctx, createReq(lab.PoolID, at(0), 0, 60, "B"))
	require.NoError(t, err)
	slotB := slotIDByNote(t, respB.Msg.GetResult(), "B")
	respC, err := head.CreateSlot(ctx, createReq(lab.PoolID, at(0), 0, 60, "C"))
	require.NoError(t, err)
	slotC := slotIDByNote(t, respC.Msg.GetResult(), "C")

	// A and B run at 0 on the two resources; C queues at 60 behind one of them.
	rowA, rowB, rowC := h.slot(t, slotA), h.slot(t, slotB), h.slot(t, slotC)
	assert.True(t, rowA.Actual.Equal(at(0)))
	assert.True(t, rowB.Actual.Equal(at(0)))
	assert.NotEqual(t, rowA.Resource, rowB.Resource, "A and B fan out onto different resources")
	assert.True(t, rowC.Actual.Equal(at(60)), "C queues behind a front slot")

	// The two front slots clock in.
	_, err = m1.ClockIn(ctx, connect.NewRequest(&v1.ClockInRequest{SlotId: slotA}))
	require.NoError(t, err)
	_, err = m2.ClockIn(ctx, connect.NewRequest(&v1.ClockInRequest{SlotId: slotB}))
	require.NoError(t, err)
	assert.Equal(t, "ACTIVE", h.slot(t, slotA).Status)
	assert.Equal(t, "ACTIVE", h.slot(t, slotB).Status)

	// A finishes early at 30; C pulls forward onto A's freed resource at 30.
	h.clock.set(at(30))
	resp, err := m1.ClockOut(ctx, connect.NewRequest(&v1.ClockOutRequest{SlotId: slotA}))
	require.NoError(t, err)

	freedResource := h.slot(t, slotA).Resource
	rowC = h.slot(t, slotC)
	assert.Equal(t, "COMPLETE", h.slot(t, slotA).Status)
	assert.True(t, rowC.Actual.Equal(at(30)), "C pulled forward to the freed time")
	assert.Equal(t, freedResource, rowC.Resource, "C took the freed resource")
	assert.Equal(t, "ACTIVE", h.slot(t, slotB).Status, "B keeps running, untouched")
	assert.True(t, positionFor(t, resp.Msg.GetResult(), slotC).GetRecommitted())
}
