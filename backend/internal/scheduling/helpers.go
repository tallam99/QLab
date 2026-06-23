package scheduling

import (
	"time"

	"github.com/tallam99/qlab/backend/internal/store"
)

// cloneSlots returns a copy of the slice so an event can mutate its working set
// without touching the locked state. store.Slot is a flat value type, so a slice
// copy is a deep copy.
func cloneSlots(in []store.Slot) []store.Slot {
	return append([]store.Slot(nil), in...)
}

// findSlot returns a pointer to the slot with id in slots (so the caller can
// mutate it in place), or nil if absent.
func findSlot(slots []store.Slot, id string) *store.Slot {
	for i := range slots {
		if slots[i].ID == id {
			return &slots[i]
		}
	}
	return nil
}

// nextPriority returns one past the highest live priority — the slot_priority for
// a newly booked slot, appended at the back of the queue. bigint leaves ample room
// (inserting between is a future concern).
func nextPriority(slots []store.Slot) int64 {
	var max int64
	for _, s := range slots {
		if s.Status.IsLive() && s.Priority > max {
			max = s.Priority
		}
	}
	return max + 1
}

// nextInLineUser returns the booker of the lowest-priority live SCHEDULED slot
// (excluding excludeID), i.e. the user authorized to reclaim a held resource. For
// a force-no-show the lapsed target is excluded so the user behind it is the
// reclaimer. (A simplification for Phase 7: a single pool-wide next-in-line rather
// than per-resource.)
func nextInLineUser(slots []store.Slot, excludeID string) (string, bool) {
	var best *store.Slot
	for i := range slots {
		s := &slots[i]
		if s.Status != store.SlotStatusScheduled || s.ID == excludeID {
			continue
		}
		if best == nil || s.Priority < best.Priority {
			best = s
		}
	}
	if best == nil {
		return "", false
	}
	return best.UserID, true
}

// resourceHasActive reports whether any ACTIVE slot currently holds resourceID.
func resourceHasActive(slots []store.Slot, resourceID string) bool {
	for _, s := range slots {
		if s.Status == store.SlotStatusActive && s.ResourceID == resourceID {
			return true
		}
	}
	return false
}

// firstFreeResource returns the lowest-id resource with no ACTIVE slot on it (the
// resources arrive id-ordered from the store), for pinning a clock-in.
func firstFreeResource(resources []store.Resource, slots []store.Slot) (string, bool) {
	for _, r := range resources {
		if !resourceHasActive(slots, r.ID) {
			return r.ID, true
		}
	}
	return "", false
}

// overrunning reports whether an ACTIVE slot is past its scheduled end (its
// occupancy [actualStart, actualStart+duration) has elapsed) — the precondition
// for a poke or force-clock-out.
func overrunning(sl store.Slot, now time.Time) bool {
	end := sl.ActualStart.Add(time.Duration(sl.DurationMinutes) * time.Minute)
	return now.After(end)
}

// laterOf returns the later of two instants.
func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
