package scheduling

import "time"

// ClockInGrace is how long after a slot's placed start a user may still clock in
// before the slot becomes NO_SHOW (§2.3, decision §12.5).
//
// Note the boundary: the pure engine does NOT read this. The service layer uses
// it to decide a slot has lapsed, mutates the status to NO_SHOW, and *then* calls
// Reschedule. Grace lives in the domain because it is a domain rule, not because
// Reschedule consumes it.
const ClockInGrace Minutes = 15

// Input is the world handed to the engine for one pool: every slot in the pool
// (open, pinned, and ignored history alike), the pool's benches, and the current
// instant. All slots and benches share Input.PoolID (§1). The engine reads no
// clock and mutates nothing — Input is the entire world it sees, and Now is
// injected (§10).
type Input struct {
	PoolID  PoolID
	Slots   []Slot
	Benches []Bench
	Now     time.Time
}

// Validate checks the structural preconditions Reschedule assumes: consistent
// pool scoping, positive durations, non-negative windows, and pinned slots
// carrying the placement data the engine needs (AssignedBench + ProjectedEnd).
// It is the caller's guard at the edge; Reschedule assumes a valid Input. A
// violation is a data/programmer bug, not a user-facing schedule failure
// (contrast §8, where the schedule itself can never be infeasible).
func (in Input) Validate() error {
	panic("not implemented")
}

// Scheduler recomputes a pool's schedule. It exists as an interface so the
// service layer can depend on the behavior (and substitute a double in handler
// tests) rather than on a concrete type; the production implementation is the
// pure Reschedule function, adapted by Engine.
type Scheduler interface {
	Reschedule(in Input) Schedule
}

// Engine is the default Scheduler: a stateless adapter so the service layer can
// hold a Scheduler dependency. All logic is in the pure Reschedule function.
type Engine struct{}

// NewScheduler returns the production Scheduler.
func NewScheduler() Scheduler { return Engine{} }

// Reschedule satisfies Scheduler by delegating to the pure package function.
func (Engine) Reschedule(in Input) Schedule { return Reschedule(in) }

// Reschedule is the whole engine: a greedy, priority-ordered, multi-bench list
// scheduler with gap-fill (§5). It recomputes the placement of every SCHEDULED
// slot, scheduling around pinned ACTIVE slots, and returns the result. It is
// pure and deterministic — identical Input yields identical Schedule (§4
// invariant 6) — and it never fails (§8).
//
// Outline (see ALGORITHM §5 for the full spec; the internal free-interval
// representation is deliberately left to the implementation step):
//
//  1. Seed each bench's free intervals from Now and from pinned ACTIVE occupancy
//     ([ActualStart, ProjectedEnd)), open-ended at the tail.
//  2. In (Priority, SlotID) order, place each SCHEDULED slot at the earliest
//     feasible start across all benches that fits its full Duration, no earlier
//     than max(WinStart, Now). Gap-fill and bench fan-out fall out of this: a
//     short slot drops into a short gap; a slot whose own bench is busy hops to a
//     free one (§5.1, §1.4).
//  3. Ratchet WinStart and flag Recommitted for any slot forced past its band;
//     leave it untouched when the slot was silently accommodated (§2.1, §2.2).
func Reschedule(in Input) Schedule {
	panic("not implemented")
}
