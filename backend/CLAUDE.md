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
  build the server → register dependencies (`s.InjectDependency`) → `s.Run(ctx)`.
  The server owns the lifecycle; no business logic here.
- `internal/config/` — the *only* place env vars are read (envconfig): `PORT`,
  `QLAB_ENV`, `DATABASE_URL`, `CORS_ALLOWED_ORIGINS`. Holds the `Environment` enum
  (generated `String()`/parse via enumer).
- `internal/logging/` — the `Logger` interface (`interface.go`) and a `Noop()`
  logger; the slog-backed implementation is in `logging/slog` (mirrors
  `store`/`store/pgstore`). The server and middleware depend on the interface, so a
  fake/alternate backend swaps in.
- `internal/clients/` — external client-tech *connection* setup only (no data
  access). `clients/postgres` builds the pgx pool; Firebase/storage clients become
  siblings later.
- `internal/store/` — the data store: the business `Store` interface
  (`interface.go`, business-domain methods only — empty until Phase 4), implemented
  by `store/pgstore`. `pgstore.New` pings on construction, so a returned store is
  guaranteed ready and nothing re-checks it (no health methods on the interface);
  `pgstore.Store` is an `io.Closer` so the server can drain its pool on shutdown.
- `internal/httpmw/` — HTTP middleware: request-id structured logging
  (`RequestLogger`, `LoggerFromContext`), panic recovery (`Recoverer`), and CORS
  (`CORS`) for the cross-origin PWA. `CORS` fails closed on an empty allow-list
  (same-origin only) rather than the underlying library's "allow all" default;
  origins come from config (`CORS_ALLOWED_ORIGINS`, set per environment to the
  Firebase Hosting origin).
- `internal/server/` — the server: router, handlers (methods on `Server`), and the
  lifecycle. `New` returns a `*Server`; `Run(ctx)` serves immediately (so
  `/healthz` liveness is 200 at once), runs the registered dependency injectors
  (`InjectDependency` / `WithPostgres`, private `initPgStore`), calls `Ready()` to
  flip `/readyz` to 200, then drains and closes deps on shutdown. `/readyz` is 503
  until ready.

## Conventions

- **`internal/` for everything not meant to be imported externally.**
- **Structured logging via the `logging.Logger` interface** (slog-backed), never
  `fmt.Println`/`log`. Use the request-scoped logger from
  `httpmw.LoggerFromContext(ctx)` inside handlers so lines carry the request id.
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
  and log attribute keys are package-level consts so they're grep-able and
  changeable in one place. **Log messages are the exception: keep them inline
  unless the same message is emitted from more than one site.**
- **Programmer-error invariants panic** (e.g. `LoggerFromContext` with no logger,
  `server.New` with a nil required dependency); the `Recoverer` middleware turns
  request-time panics into a logged 500 rather than a crash.
- **The server owns its lifecycle** (`Run(ctx)`): it starts listening *before*
  dependencies initialize, so `/healthz` is up immediately and the platform never
  mistakes slow startup for a dead container. `Run` then runs the registered
  injectors (their constructors verify health — e.g. `pgstore.New` pings — with
  bounded retry to ride out transient failures), calls `Ready()` to flip `/readyz`
  to 200, and on shutdown drains the HTTP server then closes dependencies.
- **Add dependencies via `InjectDependency`, not new setter methods.** A dependency
  is a `func(ctx, *Server) error` that initializes it (its constructor verifies
  health) and attaches it — registered before `Run`, executed inside it. This keeps
  the `Server` method set fixed as dependencies grow. Construction-time deps (the
  logger) still go in `Options`, validated in `New`. Deps are interface-typed where
  practical so tests and infra can swap implementations.
- The **scheduling engine (`internal/scheduling`, Phase 6) is pure**: no DB, no
  HTTP, no clock reads. Read `docs/ALGORITHM.md` before touching it.
- From the **repo root**, run `go build ./backend/...`, `go vet ./backend/...`, and
  `mage test` (all tiers; or `mage testUnit` for just the Go suite) before
  presenting or committing; `gofmt` everything. Re-run `go generate ./backend/...`
  after changing an enum.

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
