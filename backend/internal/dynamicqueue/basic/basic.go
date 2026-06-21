// Package basic is the first implementation of dynamicqueue.Algorithm: the
// greedy, priority-ordered, multi-resource list scheduler with gap-fill specified
// in docs/ALGORITHM.md §5. Keeping it behind the dynamicqueue.Algorithm interface
// lets the service dispatch to a different algorithm later without changing callers.
package basic

import "github.com/tallam99/qlab/backend/internal/dynamicqueue"

// Config constructs an Engine. The clock-in grace period is injected here rather
// than read from a clock or hardcoded, so the engine stays agnostic to
// configuration — the service wires it from its environment (ALGORITHM §2.3, §10).
type Config struct {
	ClockInGrace dynamicqueue.Minutes
}

// Engine is the basic scheduling algorithm. It is stateless apart from its
// configured grace period and is safe to reuse across calls.
type Engine struct {
	clockInGrace dynamicqueue.Minutes
}

// New returns a basic Engine as a dynamicqueue.Algorithm.
func New(cfg Config) dynamicqueue.Algorithm {
	return Engine{clockInGrace: cfg.ClockInGrace}
}

// Reschedule recomputes the pool's queue (ALGORITHM §5): validate the input,
// sweep no-shows (scheduled slots whose CommittedStart + grace is before
// Input.Now), then place the remaining open slots in SlotPriority order at the
// earliest feasible start across resources, recording a Trace throughout. The
// scheduling body is implemented next.
func (e Engine) Reschedule(in dynamicqueue.Input) (dynamicqueue.Result, error) {
	if err := in.Validate(); err != nil {
		return dynamicqueue.Result{}, err
	}
	_ = e.clockInGrace // consumed by the no-show sweep, implemented next
	panic("not implemented")
}
