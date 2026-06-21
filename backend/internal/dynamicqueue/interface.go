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
// resources, and the current instant. The clock-in grace period is deliberately
// NOT here: it is configuration the implementation is constructed with, not
// world-state (§10).
type Input struct {
	ResourcePoolID ResourcePoolID
	Slots          []Slot
	Resources      []Resource
	Now            time.Time
}

// Result is what Reschedule returns: the recomputed queue and the trace of how
// the engine got there.
type Result struct {
	Queue Queue
	Trace Trace
}
