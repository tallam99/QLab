# backend — orientation for Claude

The Go service: the Connect-RPC API and the scheduling engine. The repo is a
single Go module rooted at the repo top (`github.com/tallam99/qlab`); this code
lives under the `backend/` subtree, imported as `github.com/tallam99/qlab/backend/…`.

Read the root `CLAUDE.md` and `docs/PLAN.md` first for the phase boundary and the
local-vs-cloud rule. **Current status: Phase 2** — the HTTP service plus a local
Docker Compose stack (Postgres) driven by `mage`. The data model and query layer
land in Phase 4.

## Key files

- `cmd/server/main.go` — entrypoint. Keep it thin: load config → build logger →
  build store → build handler → serve with graceful shutdown. No business logic here.
- `internal/config/` — the *only* place env vars are read (envconfig). Holds the
  `Environment` enum (generated `String()`/parse via enumer).
- `internal/logging/` — slog setup (text locally, JSON in cloud).
- `internal/clients/` — external client-tech *connection* setup only (no data
  access). `clients/postgres` builds the pgx pool; Firebase/storage clients become
  siblings later.
- `internal/store/` — the data store: the business `Store` interface
  (`interface.go`), implemented by `store/pgstore` over the pool. `pgstore.New`
  health-checks at construction (so a Store handed onward is already verified); the
  store also reports ongoing readiness via `Ready`. Query methods grow here in
  Phase 4.
- `internal/httpmw/` — HTTP middleware: request-id structured logging
  (`RequestLogger`, `LoggerFromContext`) and panic recovery (`Recoverer`).
- `internal/server/` — chi router and handlers (methods on `Server`): `/healthz`
  (liveness) and `/readyz` (readiness — asks the store via `store.Ready`).

## Conventions

- **`internal/` for everything not meant to be imported externally.**
- **slog everywhere**, never `fmt.Println`/`log`. Use the request-scoped logger
  from `httpmw.LoggerFromContext(ctx)` inside handlers so lines carry the request id.
- **chi** for routing/middleware; Connect-RPC handlers mount on the same router
  later. Wire format is protobuf via Connect — generated types only, no
  hand-written request/response shapes (lands Phase 5).
- **Constructors take an `Options` (or `Config`) struct, not loose params** — so
  signatures don't churn as dependencies grow (`logging.New(Options)`,
  `server.New(Options)`).
- **Avoid abbreviations** in names (identifiers, enum values, config keys) for
  clarity — e.g. `EnvProduction`, not `EnvProd`. The exception is a well-known
  acronym that's documented (e.g. `HTTP`, `JWT`, `id`).
- **Enums**: integer type with generated `String()`/parse (enumer,
  `//go:generate`). Name values `EnumName<Value>` (e.g. `EnvLocal`). The zero
  value is `EnumNameUnknown` and is **never valid** — seeing it in a logical flow
  is a programmer error. Reject it when decoding external input.
- **Don't hardcode strings in logic** — route paths, header names, content types,
  and slog attribute keys are package-level consts so they're grep-able and
  changeable in one place. **Log messages are the exception: keep them inline
  unless the same message is emitted from more than one site.**
- **Programmer-error invariants panic** (e.g. `LoggerFromContext` with no logger,
  `server.New` with a nil required dependency); the `Recoverer` middleware turns
  request-time panics into a logged 500 rather than a crash.
- **Constructors take dependencies as interfaces via an `Options` struct.** The
  service's dependencies (store, and later cache/queue/bucket/…, plus the logger)
  are passed in already constructed and *ready*; the service uses them through
  their interfaces and never configures them. Interface-typed fields let tests and
  infra swap implementations — including no-op/stub — freely, so behavior is chosen
  by the caller's implementation, not by server-side defaulting. Required
  dependencies are validated in `New`.
- The **scheduling engine (`internal/scheduling`, Phase 6) is pure**: no DB, no
  HTTP, no clock reads. Read `docs/ALGORITHM.md` before touching it.
- From the **repo root**, run `go build ./backend/...`, `go vet ./backend/...`, and
  `mage testUnit` before presenting or committing; `gofmt` everything. Re-run
  `go generate ./backend/...` after changing an enum.

## Testing

- **`server` package tests are strictly infrastructural** — liveness/readiness
  and server lifecycle/wiring only. Endpoint *functionality* belongs in dedicated
  integration suites that exercise the full stack (DB + engine + API), not here.
- One focused, table-driven test per behavior, in a file mirroring its source
  (`health_test.go` covers the `/healthz` liveness probe).
- **Tag tests by the infrastructure they need.** Unit tests (no infra) carry
  `//go:build testunit` and run via `mage testUnit`. Integration/database suites
  get their own tags (e.g. `integration`, `database`) as they land, so each tier
  runs only where its infra exists.

## Verify the service

The service now requires `DATABASE_URL` and pings Postgres on boot, so run it
through the Compose stack rather than bare:

    mage startStack                        # from repo root: API + Postgres
    curl localhost:8090/healthz            # {"status":"ok"}  (liveness)
    curl localhost:8090/readyz             # {"status":"ok"}  (readiness — DB reachable)

To run the binary directly, point it at a Postgres instance:

    DATABASE_URL=postgres://qlab:qlab@localhost:5432/qlab?sslmode=disable go run ./backend/cmd/server
