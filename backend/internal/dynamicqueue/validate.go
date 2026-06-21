package dynamicqueue

import (
	"errors"
	"fmt"
	"time"
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
		if r.ResourcePoolID != in.ResourcePoolID {
			return fmt.Errorf("dynamicqueue: resource %q is in resource pool %q, not %q", r.ID, r.ResourcePoolID, in.ResourcePoolID)
		}
	}

	// SlotPriority is a unique total order and the sole tie-break key (§4), so
	// duplicates are a data bug the engine must not silently resolve.
	priorities := make(map[SlotPriority]SlotID, len(in.Slots))
	activeResources := make(map[ResourceID]SlotID)
	for _, s := range in.Slots {
		// Sequential guard clauses (not a tagless switch) so each condition sits in
		// its own coverage block — otherwise Go attributes coverage to the case
		// bodies and mutation testing can't see the conditions as exercised.
		if s.ResourcePoolID != in.ResourcePoolID {
			return fmt.Errorf("dynamicqueue: slot %q is in resource pool %q, not %q", s.ID, s.ResourcePoolID, in.ResourcePoolID)
		}
		if s.Duration <= 0 {
			return fmt.Errorf("dynamicqueue: slot %q has non-positive duration %d", s.ID, s.Duration)
		}
		if s.Lookahead < 0 {
			return fmt.Errorf("dynamicqueue: slot %q has negative lookahead %d", s.ID, s.Lookahead)
		}
		if !s.Status.IsOpen() && !s.Status.IsPinned() {
			return fmt.Errorf("dynamicqueue: slot %q has status %s; the engine accepts only SCHEDULED or ACTIVE", s.ID, s.Status)
		}
		if s.Status.IsPinned() {
			if !s.AssignedResource.IsAssigned() {
				return fmt.Errorf("dynamicqueue: active slot %q has no assigned resource", s.ID)
			}
			// An ACTIVE slot still holds its resource; its ProjectedEnd is when it is
			// expected to free it and must be strictly in the future. A projection
			// at or before now means it is overrunning and the caller must re-project
			// it (or record the clock-out) before rescheduling — otherwise the engine
			// would treat a still-occupied resource as free (§2.3, §6).
			if !s.ProjectedEnd.After(in.Now) {
				return fmt.Errorf("dynamicqueue: active slot %q projected end %s is not after now %s; re-project on overrun",
					s.ID, s.ProjectedEnd.Format(time.RFC3339), in.Now.Format(time.RFC3339))
			}
			// A resource runs one experiment at a time, so two ACTIVE slots cannot
			// share a resource — it would also make seeded occupancy ambiguous.
			if other, dup := activeResources[s.AssignedResource]; dup {
				return fmt.Errorf("dynamicqueue: resource %q has two active slots, %q and %q; a resource runs one at a time", s.AssignedResource, other, s.ID)
			}
			activeResources[s.AssignedResource] = s.ID
		}
		if other, dup := priorities[s.SlotPriority]; dup {
			return fmt.Errorf("dynamicqueue: slots %q and %q share priority %d; it must be a unique total order", other, s.ID, s.SlotPriority)
		}
		priorities[s.SlotPriority] = s.ID
	}
	return nil
}
