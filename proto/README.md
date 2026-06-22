# proto

The protobuf contract — the schema-of-record for the API and SSE events. Go server
stubs and TypeScript client types are generated from here via `buf`; the wire
shapes are never hand-written.

## Layout

    qlab/v1/types.proto     Lab, User, Membership, ResourcePool, Resource, Slot,
                              SlotPosition (reschedule outcome), QueueEvent (SSE
                              envelope), and the enums (LabRole, ResourceKind,
                              SlotStatus incl. NO_SHOW, Outcome, QueueEventType)
    qlab/v1/service.proto   QlabService — ListSlots + the side-effecting RPCs
                              (CreateSlot / ClockIn / ClockOut / CancelSlot, plus
                              the user-driven reclaim pair PokeOccupant /
                              ForceClockOut)
    buf.yaml                module config: protovalidate dep, lint (STANDARD),
                              breaking (FILE)
    buf.gen.yaml            codegen: Go (go-tool plugins) → backend/internal/protogen,
                              TS (npm protoc-gen-es) → frontend/src/protogen
    buf.lock                pinned dependency digests
    package.json            tooling-only: pins the TS codegen plugin (not the
                              frontend app — that lands in Phase 9)

The messages mirror the pure scheduling-engine domain
(`backend/internal/dynamicqueue`) and the persisted schema (`backend/migrations`),
so proto ⇄ domain ⇄ row conversions stay mechanical at the edges (the engine and DB
layers never import generated code; conversions live in the handlers, Phase 7).

## Generating

    cd proto && npm install      # once, installs the TS codegen plugin
    mage genProto                # buf generate (from repo root)

`mage genProto` runs `buf generate --include-imports` from `proto/`. The Go plugins
(`protoc-gen-go`, `protoc-gen-connect-go`) are the module's pinned `go tool`
binaries (go.mod `tool` directives); the TS plugin (`protoc-gen-es`) is the npm
package in `package.json`. `--include-imports` vendors the protovalidate
dependency's types into the gen dirs so both languages are self-contained; the
well-known types stay external (Go `timestamppb`, TS `@bufbuild/protobuf`).

## Conventions

- `.proto` is the single source of truth. **Generated code is committed** (so the
  consumer builds don't depend on `buf`); a CI job runs `buf lint`, `buf breaking`
  (against `main`), and regenerates to fail on drift.
- Validation rules via `protovalidate` (`buf.validate.field` options) — e.g.
  `lookahead_minutes ≥ 0`, `duration_minutes > 0`. The runtime validator
  interceptor is wired with the handlers (Phase 7).
- Enums carry a `*_UNSPECIFIED = 0` zero value (never valid) per buf's STANDARD
  lint; the conversion layer maps it to the domain's `…Unknown`.
- Each RPC has its own request/response message (Connect convention); mutating RPCs
  all return the recomputed `RescheduleResult`.

See `docs/PLAN.md` (Phase 6, and the "Wire format" cross-cutting decision).
