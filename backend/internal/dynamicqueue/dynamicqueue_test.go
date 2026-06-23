//go:build testunit

// Direct unit tests for the domain types and the input guard. These live in the
// dynamicqueue package (not only exercised transitively through basic) so the
// validation branches and helpers are pinned on their own.
package dynamicqueue

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)

// validInput is a minimal well-formed Input: one scheduled slot (lookahead 0) and
// one active slot on the single resource, with distinct priorities. Tests clone it
// and break exactly one thing.
func validInput() Input {
	return Input{
		ResourcePoolID: "pool-1",
		Now:            t0,
		Resources:      []Resource{{ID: "r1", ResourcePoolID: "pool-1", Kind: ResourceKindVentHood}},
		Slots: []Slot{
			{ID: "s1", ResourcePoolID: "pool-1", SlotPriority: 1, Status: SlotStatusScheduled, DesiredStart: t0, Duration: 60, Lookahead: 0},
			{ID: "s2", ResourcePoolID: "pool-1", SlotPriority: 2, Status: SlotStatusActive, AssignedResource: "r1", ActualStart: t0, ProjectedEnd: t0.Add(time.Hour), Duration: 60},
		},
	}
}

func TestValidate(t *testing.T) {
	// The baseline is valid — this also pins the boundaries (a lookahead of 0 and a
	// future ProjectedEnd must pass), so loosening either comparison fails here.
	require.NoError(t, validInput().Validate())

	// ProjectedEnd == now is the valid re-projection for an overrunning ACTIVE slot
	// ("frees imminently"): accepted, not an error (§6). Only a projection strictly
	// before now is rejected (the case below).
	atNow := validInput()
	atNow.Slots[1].ProjectedEnd = atNow.Now
	require.NoError(t, atNow.Validate())

	cases := []struct {
		name    string
		mutate  func(*Input)
		wantErr string
	}{
		{"no resources", func(in *Input) { in.Resources = nil }, "no resources"},
		{"resource in wrong pool", func(in *Input) { in.Resources[0].ResourcePoolID = "other" }, "is in resource pool"},
		{"slot in wrong pool", func(in *Input) { in.Slots[0].ResourcePoolID = "other" }, "is in resource pool"},
		{"zero duration", func(in *Input) { in.Slots[0].Duration = 0 }, "non-positive duration"},
		{"negative duration", func(in *Input) { in.Slots[0].Duration = -1 }, "non-positive duration"},
		{"negative lookahead", func(in *Input) { in.Slots[0].Lookahead = -1 }, "negative lookahead"},
		{"unknown status", func(in *Input) { in.Slots[0].Status = SlotStatusUnknown }, "only SCHEDULED or ACTIVE"},
		{"active without resource", func(in *Input) { in.Slots[1].AssignedResource = "" }, "no assigned resource"},
		{"projected end before now", func(in *Input) { in.Slots[1].ProjectedEnd = in.Now.Add(-time.Minute) }, "before now"},
		{"duplicate priority", func(in *Input) { in.Slots[1].SlotPriority = in.Slots[0].SlotPriority }, "unique total order"},
		{"two active on one resource", func(in *Input) {
			in.Slots = append(in.Slots, Slot{ID: "s3", ResourcePoolID: "pool-1", SlotPriority: 3, Status: SlotStatusActive, AssignedResource: "r1", ActualStart: t0, ProjectedEnd: t0.Add(time.Hour), Duration: 60})
		}, "two active slots"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := validInput()
			c.mutate(&in)
			err := in.Validate()
			require.Error(t, err)
			assert.Containsf(t, err.Error(), c.wantErr, "wrong error: %v", err)
		})
	}
}

func TestMinutesDuration(t *testing.T) {
	assert.Equal(t, time.Hour, Minutes(60).Duration())
	assert.Equal(t, 90*time.Minute, Minutes(90).Duration())
	assert.Equal(t, time.Duration(0), Minutes(0).Duration())
}

func TestSlotStatusClassification(t *testing.T) {
	assert.True(t, SlotStatusScheduled.IsOpen())
	assert.False(t, SlotStatusActive.IsOpen())
	assert.False(t, SlotStatusUnknown.IsOpen())

	assert.True(t, SlotStatusActive.IsPinned())
	assert.False(t, SlotStatusScheduled.IsPinned())
	assert.False(t, SlotStatusUnknown.IsPinned())
}

func TestResourceIDIsAssigned(t *testing.T) {
	assert.True(t, ResourceID("r1").IsAssigned())
	assert.False(t, ResourceID("").IsAssigned())
}

func TestSlotEarliestStart(t *testing.T) {
	// Lookahead pulls the floor earlier than desired.
	withLook := Slot{DesiredStart: t0, Lookahead: 30}
	assert.True(t, t0.Add(-30*time.Minute).Equal(withLook.EarliestStart()))
	// Lookahead 0 means the floor is the desired start.
	noLook := Slot{DesiredStart: t0, Lookahead: 0}
	assert.True(t, t0.Equal(noLook.EarliestStart()))
}
