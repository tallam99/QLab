package dynamicqueue

import "time"

//go:generate go tool enumer -type=StepKind -trimprefix=StepKind -transform=snake-upper -output=stepkind_enumer.go

// StepKind classifies one step the engine took. The zero value, StepKindUnknown,
// is never valid.
type StepKind int

const (
	StepKindUnknown      StepKind = iota
	StepKindSeedResource          // seeded a resource's availability from active occupancy / now
	StepKindReclaimable           // flagged a placed slot reclaimable (clock-in grace lapsed)
	StepKindPlace                 // placed a slot at a start on a resource
	StepKindGapFill               // placed a slot by backfilling an earlier gap
	StepKindRecommit              // a placed slot's start changed from its committed start
)

// Step is one entry in a Trace: a single decision the engine made, with enough
// structured context (which slot, which resource, the resulting instant) plus a
// human-readable detail to reconstruct a run when debugging (§10).
type Step struct {
	Kind     StepKind
	Slot     SlotID     // the slot involved, if any
	Resource ResourceID // the resource involved, if any
	At       time.Time  // the resulting instant, if any
	Detail   string     // human-readable, e.g. "gap-filled [10:00,10:30) on hood-2"
}

// Trace is the ordered list of steps an Algorithm took, returned in Result.
// Defined at the interface layer so every implementation produces it. At QLab's
// scale a run is a few dozen steps; if it ever grew, swap this accumulating slice
// for an injected step sink so steps stream out instead of accumulating (§10).
type Trace []Step
