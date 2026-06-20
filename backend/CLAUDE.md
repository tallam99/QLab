# backend — orientation for Claude

The Go service: the Connect-RPC API and the scheduling engine. Module path is
`github.com/tallam99/qlab/backend`.

Read the root `CLAUDE.md` and `docs/PLAN.md` first for the phase boundary and the
local-vs-cloud rule. **Current status: Phase 1** — a hello-world HTTP service.

## Key files

- `cmd/server/main.go` — entrypoint. Keep it thin: load config → build logger →
  build handler → serve with graceful shutdown. No business logic here.
- `internal/config/` — the *only* place env vars are read (envconfig).
- `internal/logging/` — slog setup (text locally, JSON in cloud).
- `internal/httplog/` — request-id + per-request structured logging middleware.
- `internal/server/` — chi router and handlers.

## Conventions

- **`internal/` for everything not meant to be imported externally.**
- **slog everywhere**, never `fmt.Println`/`log`. Use the request-scoped logger
  from `httplog.FromContext(ctx)` inside handlers so lines carry the request id.
- **chi** for routing/middleware; Connect-RPC handlers mount on the same router
  later. Wire format is protobuf via Connect — generated types only, no
  hand-written request/response shapes (lands Phase 5).
- The **scheduling engine (`internal/scheduling`, Phase 6) is pure**: no DB, no
  HTTP, no clock reads. Read `docs/ALGORITHM.md` before touching it.
- Run `go build ./...`, `go vet ./...`, and `go test ./...` before presenting or
  committing changes; `gofmt` everything.

## Verify the service

    go run ./cmd/server && curl localhost:8080/healthz   # {"status":"ok"}
    docker build -t qlab-api . && docker run -p 8080:8080 -e QLAB_ENV=staging qlab-api
