package dynamicqueue

import "time"

//go:generate go tool enumer -type=Outcome -trimprefix=Outcome -transform=snake-upper -output=outcome_enumer.go

// Outcome is the verdict the engine reached for one slot. The zero value,
// OutcomeUnknown, is never valid.
type Outcome int

const (
	OutcomeUnknown Outcome = iota // zero value; never valid
	OutcomePlaced                 // scheduled onto a resource at a start time
	OutcomeNoShow                 // clock-in grace lapsed; freed (§2.3)
)

// SlotPosition is the engine's decision for one slot. For OutcomePlaced the
// placement fields are set; for OutcomeNoShow they are zero (the slot was freed,
// not placed).
type SlotPosition struct {
	Outcome Outcome

	// ActualStart is where the slot now runs; occupancy is [ActualStart,
	// ActualStart+Duration]. Set only when Outcome is OutcomePlaced.
	ActualStart time.Time
	// AssignedResource is the resource the slot landed on. Set only when placed.
	AssignedResource ResourceID

	// Recommitted is true when the slot was placed and its ActualStart differs from
	// the slot's CommittedStart — the signal to notify the user of the new start,
	// earlier or later (§2.2). It is the only notify flag the engine emits; other
	// triggers (resource-freed, etc.) are derived by the caller from the diff.
	Recommitted bool
}

// Queue is the engine's output: the verdict for every slot it placed or freed —
// that is, the SCHEDULED slots it was given — keyed by slot id. ACTIVE slots are
// pinned and not included (§4 invariant 5). Order is not modeled here:
// SlotPriority carries the queue order, ActualStart the execution timeline (§1.3).
type Queue map[SlotID]SlotPosition
