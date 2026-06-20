// Package scheduling is the pure core of QLab's multi-bench scheduling engine.
//
// It implements docs/ALGORITHM.md, the schema-of-record for the scheduling
// logic. The package is deliberately pure: no database, no HTTP, no clock reads.
// The caller supplies the world (the pool's slots and benches, plus the current
// instant) and receives a recomputed Schedule; persistence, transactions,
// notifications, and time itself all live at the edges (the Phase 7 service).
//
// The whole engine is one operation, Reschedule: given the current state it
// recomputes where every open slot runs. "A delay cascaded" and "a gap pulled
// people forward" are not separate code paths — they are the same call over
// different inputs (ALGORITHM §5).
//
// The core types and how they relate:
//
//   - Slot      one booking: identity, declared flexibility, status, and current
//               placement (§1.1). Whether a field is input or output depends on
//               its Status.
//   - Bench     one interchangeable machine in a pool; the single-file queue fans
//               out across benches, so no-overlap is per bench (§1.4).
//   - Input     the world handed to the engine for one pool: its slots, its
//               benches, and Now.
//   - Schedule  the engine's output: a Placement per open slot, and whether each
//               had to be re-committed to a later start (§5).
//
// Read docs/ALGORITHM.md in full before changing anything here: the code
// implements that document, not the other way around.
package scheduling
