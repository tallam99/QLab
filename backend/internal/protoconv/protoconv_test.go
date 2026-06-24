//go:build testunit

// These tests pin the store-domain -> wire conversions shared by the public API and
// the operator surface. They matter because the mapping lives in one place precisely
// so the two transports can't diverge: every SlotStatus must map (a new enum value
// that falls through to UNSPECIFIED is a bug to catch), and the domain's "unset"
// sentinels (uuid.Nil, the zero time) must render as empty/nil on the wire.
package protoconv

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
	"github.com/tallam99/qlab/backend/internal/store"
)

// TestUUID checks uuid.Nil renders as "" (the wire's unset) and any other id renders
// as its canonical string.
func TestUUID(t *testing.T) {
	require.Equal(t, "", UUID(uuid.Nil))
	id := uuid.New()
	require.Equal(t, id.String(), UUID(id))
}

// TestTime checks the zero instant renders as a nil timestamp and a real instant
// round-trips to the same wall-clock time.
func TestTime(t *testing.T) {
	require.Nil(t, Time(time.Time{}))
	now := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	require.True(t, Time(now).AsTime().Equal(now))
}

// TestSlotStatus maps every persistence status to its wire enum and an unknown value
// to UNSPECIFIED. The table is exhaustive over the domain enum so adding a status
// without mapping it here (and in SlotStatus) fails.
func TestSlotStatus(t *testing.T) {
	tests := []struct {
		in   store.SlotStatus
		want v1.SlotStatus
	}{
		{store.SlotStatusScheduled, v1.SlotStatus_SLOT_STATUS_SCHEDULED},
		{store.SlotStatusActive, v1.SlotStatus_SLOT_STATUS_ACTIVE},
		{store.SlotStatusComplete, v1.SlotStatus_SLOT_STATUS_COMPLETE},
		{store.SlotStatusCancelled, v1.SlotStatus_SLOT_STATUS_CANCELLED},
		{store.SlotStatusNoShow, v1.SlotStatus_SLOT_STATUS_NO_SHOW},
		{store.SlotStatus(0), v1.SlotStatus_SLOT_STATUS_UNSPECIFIED}, // unknown -> unspecified, not a panic
	}
	for _, tt := range tests {
		require.Equalf(t, tt.want, SlotStatus(tt.in), "status %v", tt.in)
	}
}

// TestSlot converts a fully-populated slot and asserts each wire field, including the
// unset sentinels: a nil assigned resource becomes "", and a zero committed/actual
// start becomes a nil timestamp while desired_start is carried.
func TestSlot(t *testing.T) {
	id := uuid.New()
	lab := uuid.New()
	user := uuid.New()
	pool := uuid.New()
	desired := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)

	got := Slot(store.Slot{
		ID:               id,
		LabID:            lab,
		UserID:           user,
		ResourcePoolID:   pool,
		ResourceID:       uuid.Nil, // unset -> ""
		Priority:         3,
		Status:           store.SlotStatusScheduled,
		DesiredStart:     desired,
		LookaheadMinutes: 30,
		DurationMinutes:  60,
		CommittedStart:   time.Time{}, // unset -> nil
		ActualStart:      time.Time{}, // unset -> nil
		Note:             "n",
	})

	require.Equal(t, id.String(), got.GetId())
	require.Equal(t, lab.String(), got.GetLabId())
	require.Equal(t, user.String(), got.GetUserId())
	require.Equal(t, pool.String(), got.GetResourcePoolId())
	require.Equal(t, "", got.GetAssignedResourceId())
	require.Equal(t, int32(3), got.GetSlotPriority())
	require.Equal(t, v1.SlotStatus_SLOT_STATUS_SCHEDULED, got.GetStatus())
	require.True(t, got.GetDesiredStart().AsTime().Equal(desired))
	require.Equal(t, int32(30), got.GetLookaheadMinutes())
	require.Equal(t, int32(60), got.GetDurationMinutes())
	require.Nil(t, got.GetCommittedStart())
	require.Nil(t, got.GetActualStart())
	require.Equal(t, "n", got.GetNote())

	// And an assigned resource is rendered when present.
	res := uuid.New()
	require.Equal(t, res.String(), Slot(store.Slot{ResourceID: res}).GetAssignedResourceId())
}
