package dynamicqueue

import "time"

// SlotPosition is the engine's decision for one SCHEDULED slot: where it placed the
// slot, whether that start changed, and whether the slot is now reclaimable. The
// schedule never fails (§8), so every SCHEDULED slot the engine is given is placed —
// there is no "freed" or "no-show" outcome.
type SlotPosition struct {
	// ActualStart is where the slot runs; occupancy is [ActualStart,
	// ActualStart+Duration].
	ActualStart time.Time
	// AssignedResource is the resource the slot landed on.
	AssignedResource ResourceID

	// Recommitted is true when ActualStart differs from the slot's CommittedStart —
	// the signal to notify the user of the new start, earlier or later (§2.2). It is
	// the only notify flag the engine emits; other triggers (resource-freed, etc.)
	// are derived by the caller from the diff.
	Recommitted bool

	// Reclaimable is true when the slot's clock-in grace has lapsed
	// (CommittedStart + grace < Now) and it has not clocked in (still SCHEDULED). The
	// engine does NOT free a no-show — the user may simply have forgotten to clock in
	// while using the resource, and auto-freeing would desync the schedule from
	// reality — so it keeps the slot in place and flags it. The next-in-line user may
	// then reclaim the resource via a ForceNoShow event; the terminal NO_SHOW status
	// is set by that human action, not the engine (§2.3). The grace lapse is the
	// implicit warning, so unlike an overrun reclaim no separate poke precedes it.
	Reclaimable bool
}

// Queue is the engine's output: the placement for every SCHEDULED slot it was
// given, keyed by slot id. ACTIVE slots are pinned and not included (§4 invariant
// 5). Order is not modeled here: SlotPriority carries the queue order, ActualStart
// the execution timeline (§1.3).
type Queue map[SlotID]SlotPosition
