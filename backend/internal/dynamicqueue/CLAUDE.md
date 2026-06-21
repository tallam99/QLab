# dynamicqueue — orientation for Claude

The pure core of QLab's scheduling: a **dynamic priority queue** that continuously
re-flows across interchangeable resources. Implements `docs/ALGORITHM.md` — read it
in full before touching anything here; this code implements that document, not the
other way around.

**Purity (hard rule):** no DB, no HTTP, no clock reads, no `time.Now()`. The caller
injects the world (`Input`, including `Now`) and the grace period (at
construction); the engine returns a `Result`. proto ⇄ domain and DB-row ⇄ domain
conversions happen at the edges (Phase 7), never here.

## Layout

    interface.go    Algorithm (the engine contract), Input, Result
    ids.go          opaque id types (SlotID, ResourceID, …)
    slot.go         Slot, SlotStatus (SCHEDULED/ACTIVE), Minutes, SlotPriority
    resource.go     Resource, ResourceKind (MVP: vent hood)
    queue.go        Queue (map[SlotID]SlotPosition), SlotPosition, Outcome
    trace.go        Trace/Step — the per-run step log every Algorithm returns
    validate.go     Input.Validate (Reschedule runs it; callers don't)
    *_enumer.go     generated enum String()/parse (go generate)
    basic/          the first Algorithm: greedy multi-resource gap-fill (§5)

## Model cheat-sheet (see ALGORITHM.md for the why)

- A slot has a `DesiredStart` and a `Lookahead`; its earliest start is
  `DesiredStart − Lookahead`. The engine is greedy (pulls to earliest feasible) and
  pushes later only when forced. No late window, no ratchet, no silent absorption.
- Re-commit/notify fires whenever a placed slot's start differs from its
  `CommittedStart` (`SlotPosition.Recommitted`).
- The engine detects no-shows itself (`CommittedStart + grace < Now`) and emits
  `OutcomeNoShow`. Grace is constructor config (`basic.Config`), not world-state.
- `SlotPriority` is a unique total order and the sole processing/tie-break key;
  `id` is identity only.
- The engine sees only `SCHEDULED` + `ACTIVE`; the caller filters history out.

## Conventions

- Output is per-slot verdicts in the `Queue`, never mutated input `Slot`s.
- Enums follow the repo pattern: integer type, zero value `…Unknown` (never valid),
  `enumer`-generated `String()`/parse via `//go:generate`; rerun
  `go generate ./backend/internal/dynamicqueue/...` after changing one.
- Invariant assertions (§4) are **test logic**, not a runtime path — they live in
  `basic`'s test suite, not in the package.
