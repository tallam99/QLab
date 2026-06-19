# backend

The Go service: the Connect-RPC API and the scheduling engine.

> **Status:** not yet scaffolded — created in Phase 1 (see `docs/PLAN.md`).

## Layout (planned)

    cmd/server/        entrypoint (thin: config, wiring, start)
    internal/          all non-exported code
      scheduling/      the scheduling engine — PURE functions, no DB/HTTP/clock
                       (implements docs/ALGORITHM.md)
      db/              persistence (pgx, sqlc, squirrel; goose migrations)
      gen/             generated Connect/proto Go code (from proto/)
    Dockerfile         multi-stage build

## Conventions

- Keep `main.go` thin; logic lives in `internal/`.
- The scheduling engine is pure and exhaustively tested — read `docs/ALGORITHM.md`
  before touching it.
- Wire format is protobuf via Connect; types are generated from `proto/` — don't
  hand-write request/response shapes.
- `slog` for logging, OpenTelemetry spans for tracing.

See `docs/PLAN.md` (Phases 1, 6, 7) and `docs/ARCHITECTURE.md`.
