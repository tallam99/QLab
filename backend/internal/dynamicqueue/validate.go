package dynamicqueue

import (
	"errors"
	"fmt"
)

// Validate checks the structural preconditions an Algorithm assumes, so
// Reschedule can reject malformed Input rather than produce a nonsense queue. It
// is the engine's own guard — Reschedule calls it first, so callers do not need
// to. A failure here is a data/programmer bug, distinct from the schedule never
// being "infeasible" (§8): that is about placement, this is about input validity.
func (in Input) Validate() error {
	if len(in.Resources) == 0 {
		return errors.New("dynamicqueue: input has no resources")
	}
	for _, r := range in.Resources {
		if r.PoolID != in.PoolID {
			return fmt.Errorf("dynamicqueue: resource %q is in pool %q, not %q", r.ID, r.PoolID, in.PoolID)
		}
	}

	// SlotPriority is a unique total order and the sole tie-break key (§4), so
	// duplicates are a data bug the engine must not silently resolve.
	priorities := make(map[SlotPriority]SlotID, len(in.Slots))
	for _, s := range in.Slots {
		switch {
		case s.PoolID != in.PoolID:
			return fmt.Errorf("dynamicqueue: slot %q is in pool %q, not %q", s.ID, s.PoolID, in.PoolID)
		case s.Duration <= 0:
			return fmt.Errorf("dynamicqueue: slot %q has non-positive duration %d", s.ID, s.Duration)
		case s.Lookahead < 0:
			return fmt.Errorf("dynamicqueue: slot %q has negative lookahead %d", s.ID, s.Lookahead)
		case !s.Status.IsOpen() && !s.Status.IsPinned():
			return fmt.Errorf("dynamicqueue: slot %q has status %s; the engine accepts only SCHEDULED or ACTIVE", s.ID, s.Status)
		}
		if s.Status.IsPinned() {
			if !s.AssignedResource.IsAssigned() {
				return fmt.Errorf("dynamicqueue: active slot %q has no assigned resource", s.ID)
			}
			if s.ProjectedEnd.IsZero() {
				return fmt.Errorf("dynamicqueue: active slot %q has no projected end", s.ID)
			}
		}
		if other, dup := priorities[s.SlotPriority]; dup {
			return fmt.Errorf("dynamicqueue: slots %q and %q share priority %d; it must be a unique total order", other, s.ID, s.SlotPriority)
		}
		priorities[s.SlotPriority] = s.ID
	}
	return nil
}
