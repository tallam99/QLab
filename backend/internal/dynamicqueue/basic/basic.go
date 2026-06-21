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
