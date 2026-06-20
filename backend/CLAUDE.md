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
  start serving (liveness up immediately) → initialize dependencies with bounded
  retry → `MarkReady` → serve until a shutdown signal. No business logic here.
- `internal/config/` — the *only* place env vars are read (envconfig). Holds the
  `Environment` enum (generated `String()`/parse via enumer).
- `internal/logging/` — slog setup (text locally, JSON in cloud).
- `internal/clients/` — external client-tech *connection* setup only (no data
  access). `clients/postgres` builds the pgx pool; Firebase/storage clients become
  siblings later.
- `internal/store/` — the data store: the business `Store` interface
  (`interface.go`, business-domain methods only — empty until Phase 4), implemented
  by `store/pgstore`. `pgstore.New` pings on construction, so a returned store is
  guaranteed ready and nothing re-checks it (no health methods on the interface).
- `internal/httpmw/` — HTTP middleware: request-id structured logging
  (`RequestLogger`, `LoggerFromContext`) and panic recovery (`Recoverer`).
- `internal/server/` — chi router and handlers (methods on `Server`). `New`
  returns a `*Server` that serves immediately so `/healthz` (liveness) is 200 from
  the start; `/readyz` (readiness) returns 503 until `MarkReady` runs — which
  `main` calls once dependencies initialize — then 200. Runtime deps are injected
  via `MarkReady` (not `Options`), because the server listens before they exist.

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
- **Liveness up first, then readiness.** The server starts listening *before*
  dependencies initialize, so `/healthz` is up immediately and the platform never
  mistakes slow startup for a dead container. `main` then initializes dependencies
  (their constructors verify health — e.g. `pgstore.New` pings — with bounded retry
  to ride out transient failures) and calls `MarkReady`, flipping `/readyz` to 200.
- **Dependencies are injected as interfaces, already constructed and ready.** The
  service uses them through their interfaces and never configures or re-checks them.
  Construction-time deps (the logger) go in `Options` (validated in `New`, panic on
  nil); runtime deps that must be initialized first (the store, later
  cache/queue/…) are injected via `MarkReady`. Interface types let tests and infra
  swap implementations freely.
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
