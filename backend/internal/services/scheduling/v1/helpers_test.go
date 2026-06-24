//go:build testunit

// These tests pin the pure decision primitives the scheduling events are built from
// — next-in-line, overrun, free-resource selection, queue-tail priority. The events
// themselves are exercised end to end by backend/integration_test (real store +
// engine); this file isolates the branchy logic where a subtle bug (an off-by-one in
// priority, the wrong reclaimer) would hide, so it is asserted directly and fast.
package v1

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/services/scheduling"
	"github.com/tallam99/qlab/backend/internal/store"
)

var t0 = time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)

// scheduled builds a SCHEDULED slot with the given priority and owner.
func scheduled(priority int64, user uuid.UUID) store.Slot {
	return store.Slot{ID: uuid.New(), UserID: user, Priority: priority, Status: store.SlotStatusScheduled}
}

// TestNextInLineUser: the next-in-line user is the owner of the lowest-priority live
// SCHEDULED slot, with excludeID skipped (a force-no-show excludes the lapsed target
// so the user behind it is the reclaimer), and only SCHEDULED slots count.
func TestNextInLineUser(t *testing.T) {
	a, b := uuid.New(), uuid.New()

	t.Run("lowest priority scheduled wins", func(t *testing.T) {
		got, ok := nextInLineUser([]store.Slot{scheduled(2, b), scheduled(1, a)}, uuid.Nil)
		require.True(t, ok)
		require.Equal(t, a, got)
	})

	t.Run("excludeID is skipped", func(t *testing.T) {
		lapsed := scheduled(1, a)
		behind := scheduled(2, b)
		got, ok := nextInLineUser([]store.Slot{lapsed, behind}, lapsed.ID)
		require.True(t, ok)
		require.Equal(t, b, got, "excluding the front slot promotes the one behind it")
	})

	t.Run("non-scheduled slots are ignored", func(t *testing.T) {
		active := store.Slot{ID: uuid.New(), UserID: a, Priority: 1, Status: store.SlotStatusActive}
		got, ok := nextInLineUser([]store.Slot{active, scheduled(2, b)}, uuid.Nil)
		require.True(t, ok)
		require.Equal(t, b, got, "an ACTIVE slot at lower priority must not be picked")
	})

	t.Run("no scheduled slots", func(t *testing.T) {
		_, ok := nextInLineUser(nil, uuid.Nil)
		require.False(t, ok)
	})
}

// TestRequireNextInLine maps the next-in-line computation to the reclaim authorization:
// only the next-in-line user passes; anyone else (or an empty queue) is ErrForbidden.
func TestRequireNextInLine(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	slots := []store.Slot{scheduled(1, a), scheduled(2, b)}

	require.NoError(t, requireNextInLine(slots, uuid.Nil, a))
	require.ErrorIs(t, requireNextInLine(slots, uuid.Nil, b), scheduling.ErrForbidden)
	require.ErrorIs(t, requireNextInLine(nil, uuid.Nil, a), scheduling.ErrForbidden)
}

// TestNextPriority returns one past the highest LIVE priority — settled slots don't
// hold a place, so a new booking reuses the gap they leave at the tail.
func TestNextPriority(t *testing.T) {
	user := uuid.New()
	settled := store.Slot{ID: uuid.New(), UserID: user, Priority: 9, Status: store.SlotStatusComplete}
	require.Equal(t, int64(3), nextPriority([]store.Slot{scheduled(1, user), scheduled(2, user), settled}))
	require.Equal(t, int64(1), nextPriority(nil), "an empty queue starts at priority 1")
}

// TestOverrunning is true once an ACTIVE slot is strictly past its scheduled end
// (actual_start + duration), the precondition for a poke or force-clock-out.
func TestOverrunning(t *testing.T) {
	sl := store.Slot{Status: store.SlotStatusActive, ActualStart: t0, DurationMinutes: 60}
	require.False(t, overrunning(sl, t0.Add(59*time.Minute)), "before the end is not overrunning")
	require.False(t, overrunning(sl, t0.Add(60*time.Minute)), "exactly at the end is not yet overrunning")
	require.True(t, overrunning(sl, t0.Add(61*time.Minute)), "past the end is overrunning")
}

// TestFirstFreeResource returns the lowest-id resource (resources arrive id-ordered)
// with no ACTIVE slot on it, or false when every resource is occupied.
func TestFirstFreeResource(t *testing.T) {
	r1 := store.Resource{ID: uuid.New()}
	r2 := store.Resource{ID: uuid.New()}
	resources := []store.Resource{r1, r2}

	t.Run("first resource free", func(t *testing.T) {
		got, ok := firstFreeResource(resources, nil)
		require.True(t, ok)
		require.Equal(t, r1.ID, got)
	})

	t.Run("skips an occupied resource", func(t *testing.T) {
		occupying := store.Slot{Status: store.SlotStatusActive, ResourceID: r1.ID}
		got, ok := firstFreeResource(resources, []store.Slot{occupying})
		require.True(t, ok)
		require.Equal(t, r2.ID, got)
	})

	t.Run("none free", func(t *testing.T) {
		busy := []store.Slot{
			{Status: store.SlotStatusActive, ResourceID: r1.ID},
			{Status: store.SlotStatusActive, ResourceID: r2.ID},
		}
		_, ok := firstFreeResource(resources, busy)
		require.False(t, ok)
	})
}

// TestFindSlot returns a pointer into the slice (so callers mutate in place) or nil.
func TestFindSlot(t *testing.T) {
	slots := []store.Slot{scheduled(1, uuid.New())}
	require.Same(t, &slots[0], findSlot(slots, slots[0].ID))
	require.Nil(t, findSlot(slots, uuid.New()))
}

// TestLaterOf returns the later of two instants (used to floor a placement at now).
func TestLaterOf(t *testing.T) {
	earlier, later := t0, t0.Add(time.Hour)
	require.Equal(t, later, laterOf(earlier, later))
	require.Equal(t, later, laterOf(later, earlier))
}
