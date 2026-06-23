// Package basic is the first implementation of dynamicqueue.Algorithm: the
// greedy, priority-ordered, multi-resource list scheduler with gap-fill specified
// in docs/ALGORITHM.md §5. Keeping it behind the dynamicqueue.Algorithm interface
// lets the service dispatch to a different algorithm later without changing callers.
package basic

import "github.com/tallam99/qlab/backend/internal/dynamicqueue"

// Engine is the basic scheduling algorithm. It is stateless — the clock-in grace
// period arrives per run on dynamicqueue.Input (§2.3, §10) rather than being held
// here — so it is safe to reuse across calls.
type Engine struct{}

// New returns a basic Engine as a dynamicqueue.Algorithm.
func New() dynamicqueue.Algorithm { return Engine{} }
