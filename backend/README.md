# backend

The Go service: the Connect-RPC API and the scheduling engine.

> **Status:** Phase 2 — an HTTP service with `/healthq` (liveness, up immediately)
> and `/readyq` (readiness, 503 until dependencies initialize), structured logging,
> a boot-time DB connection with bounded retry, and a multi-stage Docker build. The
> Connect-RPC API, query layer, and engine land in later phases (see `docs/PLAN.md`).

## Layout

    cmd/server/        entrypoint (thin: config, logger, build server, register deps, run)
    internal/
      config/          env-driven config (envconfig): PORT, QLAB_ENV, DATABASE_URL,
                       CORS_ALLOWED_ORIGINS; Environment enum (String()/parse via enumer)
      logging/         Logger interface + Noop()
        slog/          slog-backed implementation (text locally, JSON in cloud)
      clients/         external client-tech setup (connection only)
        postgres/      pgx connection pool
      store/           data store: business interface (interface.go) …
        pgstore/       …and its Postgres-backed implementation (io.Closer)
      httpmw/          HTTP middleware: request-id logging, panic recovery, CORS
      server/          router, handlers, and lifecycle (New + Run; /healthq, /readyq)
    migrations/        goose migrations (empty until Phase 5)
    Dockerfile         multi-stage build → distroless/static

Planned additions (later phases):

    internal/
      scheduling/      the scheduling engine — PURE functions, no DB/HTTP/clock
                       (implements docs/ALGORITHM.md)
      store/           query methods (sqlc, squirrel) grow on the interface
      clients/         firebase, object storage, … as they're needed
      gen/             generated Connect/proto Go code (from proto/)

## Run it

The service requires `DATABASE_URL` and pings Postgres on boot, so the normal path
is the Compose stack:

    mage startStack                     # from repo root: API + Postgres
    curl localhost:8090/healthq         # -> {"status":"ok"}  (liveness)
    curl localhost:8090/readyq          # -> {"status":"ok"}  (readiness — 503 until deps init, then 200)

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
