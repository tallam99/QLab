// Package dynamicqueue defines the contract and domain types for QLab's
// dynamic-queue scheduling engine: the priority queue that continuously re-flows
// across interchangeable resources (see docs/ALGORITHM.md). It is pure — no DB,
// no HTTP, no clock reads. Implementations live in subpackages (the first is
// basic). Fuller orientation is in CLAUDE.md.
package dynamicqueue

import "time"

// Algorithm recomputes a resource pool's queue from the current state of the
// world. Implementations are pure and deterministic; they validate their Input
// and never report the schedule itself as infeasible (ALGORITHM §8) — the error
// is reserved for malformed Input. Every implementation also returns a Trace of
// the steps it took, so any run can be reconstructed when debugging (§10).
type Algorithm interface {
	Reschedule(in Input) (Result, error)
}

// Input is the world handed to the engine for one pool: the live slots
// (SCHEDULED + ACTIVE — the caller filters history out, §1.2), the pool's
// resources, the current instant, and the clock-in grace period. Grace is passed
// per run rather than held by the implementation so the caller (the scheduling
// service) is the single owner of the value and the engine stays config-free —
// the engine reads grace as world-state, exactly like Now (§2.3, §10).
type Input struct {
	ResourcePoolID ResourcePoolID
	Slots          []Slot
	Resources      []Resource
	Now            time.Time
	// Grace is the clock-in grace period: a SCHEDULED slot whose
	// CommittedStart + Grace is before Now (and which hasn't clocked in) is flagged
	// Reclaimable. Whole minutes, >= 0; 0 means a slot is reclaimable the instant it
	// is past its committed start.
	Grace Minutes
}

// Result is what Reschedule returns: the recomputed queue and the trace of how
// the engine got there.
type Result struct {
	Queue Queue
	Trace Trace
}
