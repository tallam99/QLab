# proto

The protobuf contract — the schema-of-record for the API and SSE events. Go server
stubs and TypeScript client types are generated from here via `buf`.

> **Status:** not yet populated — created in Phase 5 (see `docs/PLAN.md`).

## Layout (planned)

    qlab/v1/         versioned package: Lab, User, BenchPool, Slot, Membership,
                       Slot.Status enum, SSE event envelope, reschedule result
    buf.yaml           module config
    buf.gen.yaml       codegen targets (Go → backend/internal/gen, TS → frontend/src/gen)

## Conventions

- `.proto` is the single source of truth; generate with `mage proto` (`buf generate`).
- `buf lint` + `buf breaking` gate changes in CI.
- Validation rules via `protovalidate` (e.g. `window ≥ 0`).

See `docs/PLAN.md` (Phase 5, and the "Wire format" cross-cutting decision).
