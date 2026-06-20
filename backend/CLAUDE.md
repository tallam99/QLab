# backend — orientation for Claude

The Go service: the Connect-RPC API and the scheduling engine. Module path is
`github.com/tallam99/qlab/backend`.

Read the root `CLAUDE.md` and `docs/PLAN.md` first for the phase boundary and the
local-vs-cloud rule. **Current status: Phase 1** — a hello-world HTTP service.

## Key files

- `cmd/server/main.go` — entrypoint. Keep it thin: load config → build logger →
  build handler → serve with graceful shutdown. No business logic here.
- `internal/config/` — the *only* place env vars are read (envconfig). Holds the
  `Environment` enum (generated `String()`/parse via enumer).
- `internal/logging/` — slog setup (text locally, JSON in cloud).
- `internal/httpmw/` — HTTP middleware: request-id structured logging
  (`RequestLogger`, `FromContext`) and panic recovery (`Recoverer`).
- `internal/server/` — chi router and handlers (currently `/healthz`).

## Conventions

- **`internal/` for everything not meant to be imported externally.**
- **slog everywhere**, never `fmt.Println`/`log`. Use the request-scoped logger
  from `httpmw.FromContext(ctx)` inside handlers so lines carry the request id.
- **chi** for routing/middleware; Connect-RPC handlers mount on the same router
  later. Wire format is protobuf via Connect — generated types only, no
  hand-written request/response shapes (lands Phase 5).
- **Constructors take an `Options` (or `Config`) struct, not loose params** — so
  signatures don't churn as dependencies grow (`logging.New(Options)`,
  `server.New(Options)`).
- **Enums**: integer type with generated `String()`/parse (enumer,
  `//go:generate`). Name values `EnumName<Value>` (e.g. `EnvLocal`). The zero
  value is `EnumNameUnknown` and is **never valid** — seeing it in a logical flow
  is a programmer error. Reject it when decoding external input.
- **Don't hardcode strings in logic** — route paths, header names, content types,
  log messages, and slog attribute keys are package-level consts so they're
  grep-able and changeable in one place.
- **Programmer-error invariants panic** (e.g. `FromContext` with no logger); the
  `Recoverer` middleware turns panics into a logged 500 rather than a crash.
- The **scheduling engine (`internal/scheduling`, Phase 6) is pure**: no DB, no
  HTTP, no clock reads. Read `docs/ALGORITHM.md` before touching it.
- Run `go build ./...`, `go vet ./...`, and `go test ./...` before presenting or
  committing changes; `gofmt` everything. Re-run `go generate ./...` after
  changing an enum.

## Testing

- **`server` package tests are strictly infrastructural** — liveness/readiness
  and server lifecycle/wiring only. Endpoint *functionality* belongs in dedicated
  integration suites that exercise the full stack (DB + engine + API), not here.
- One focused, table-driven test per behavior, in a file mirroring its source
  (`health_test.go` covers the `/healthz` liveness probe).

## Verify the service

    go run ./cmd/server && curl localhost:8090/healthz   # {"status":"ok"}
    docker build -t qlab-api . && docker run -p 8090:8090 -e QLAB_ENV=staging qlab-api
