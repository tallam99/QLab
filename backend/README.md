# backend

The Go service: the Connect-RPC API and the scheduling engine.

> **Status:** Phase 1 — a hello-world HTTP service with `/healthz`, structured
> logging, and a multi-stage Docker build. The API, DB, and engine land in later
> phases (see `docs/PLAN.md`).

## Layout

    cmd/server/        entrypoint (thin: config, logger, wiring, start)
    internal/
      config/          env-driven config (envconfig): PORT, QLAB_ENV
      logging/         slog logger (text locally, JSON in cloud)
      httplog/         request-id + per-request structured log middleware
      server/          chi router + handlers (currently /healthz)
    Dockerfile         multi-stage build → distroless/static

Planned additions (later phases):

    internal/
      scheduling/      the scheduling engine — PURE functions, no DB/HTTP/clock
                       (implements docs/ALGORITHM.md)
      db/              persistence (pgx, sqlc, squirrel; goose migrations)
      gen/             generated Connect/proto Go code (from proto/)

## Run it

    go run ./cmd/server                 # listens on :8080 (override with PORT)
    curl localhost:8080/healthz         # -> {"status":"ok"}

    docker build -t qlab-api .          # multi-stage build
    docker run -p 8080:8080 -e QLAB_ENV=staging qlab-api

`QLAB_ENV=local` (the default) uses text logs; anything else uses JSON.

## Conventions

- Keep `main.go` thin; logic lives in `internal/`.
- The scheduling engine is pure and exhaustively tested — read `docs/ALGORITHM.md`
  before touching it.
- Wire format is protobuf via Connect; types are generated from `proto/` — don't
  hand-write request/response shapes.
- `slog` for logging, OpenTelemetry spans for tracing.

See `docs/PLAN.md` (Phases 1, 6, 7) and `docs/ARCHITECTURE.md`.
