# backend

The Go service: the Connect-RPC API and the scheduling engine.

> **Status:** Phase 2 — an HTTP service with `/healthz` (liveness) and `/readyz`
> (readiness, pings Postgres), structured logging, a boot-time DB connection, and
> a multi-stage Docker build. The Connect-RPC API, query layer, and engine land in
> later phases (see `docs/PLAN.md`).

## Layout

    cmd/server/        entrypoint (thin: config, logger, DB connect, wiring, start)
    internal/
      config/          env-driven config (envconfig): PORT, QLAB_ENV, DATABASE_URL;
                       Environment enum (String()/parse generated via enumer)
      logging/         slog logger (text locally, JSON in cloud)
      db/              Postgres connection pool (pgx); Phase 2 connects + pings only
      httpmw/          HTTP middleware: request-id logging + panic recovery
      server/          chi router + handlers (/healthz, /readyz)
    migrations/        goose migrations (empty until Phase 4)
    Dockerfile         multi-stage build → distroless/static

Planned additions (later phases):

    internal/
      scheduling/      the scheduling engine — PURE functions, no DB/HTTP/clock
                       (implements docs/ALGORITHM.md)
      db/              query layer (sqlc, squirrel) on top of the pool
      gen/             generated Connect/proto Go code (from proto/)

## Run it

The service requires `DATABASE_URL` and pings Postgres on boot, so the normal path
is the Compose stack:

    mage up                             # from repo root: API + Postgres
    curl localhost:8090/healthz         # -> {"status":"ok"}  (liveness)
    curl localhost:8090/readyz          # -> {"status":"ok"}  (readiness — DB reachable)

To run the binary directly, supply a database:

    DATABASE_URL=postgres://qlab:qlab@localhost:5432/qlab?sslmode=disable \
      go run ./cmd/server               # listens on :8090 (override with PORT)

`QLAB_ENV` must be one of `local` / `staging` / `production` (invalid values fail at
startup). `local` (the default) uses text logs; the others use JSON. The local
default port is `8090` to dodge other tooling (Firebase emulators, Postgres,
cloud-sql-proxy, Vite); Cloud Run overrides it via `PORT`.

## Codegen

`enumer` is pinned as a Go tool dependency (`tool` directive in `go.mod`), so no
separate install is needed:

    go generate ./...   # regenerates enum String()/parse via `go tool enumer`

## Conventions

- Keep `main.go` thin; logic lives in `internal/`.
- The scheduling engine is pure and exhaustively tested — read `docs/ALGORITHM.md`
  before touching it.
- Wire format is protobuf via Connect; types are generated from `proto/` — don't
  hand-write request/response shapes.
- `slog` for logging, OpenTelemetry spans for tracing.

See `docs/PLAN.md` (Phases 1, 6, 7) and `docs/ARCHITECTURE.md`.
