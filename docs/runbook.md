# QLab — Local Runbook

> How to run and debug the **local** stack. Claude operates this environment
> autonomously (see `CLAUDE.md`); the user drives staging/prod.

## Prerequisites

| Tool | Status | Notes |
|------|--------|-------|
| Go | ✅ installed | 1.26+ |
| Node.js | ✅ installed | current LTS |
| Docker + Compose v2 | ✅ installed | Engine (systemd) in WSL; `docker compose` |
| buf | ✅ installed | in `C:\Users\thfif\go\bin` (on PATH) |
| mage | ✅ installed | task runner |
| Python 3 | ✅ installed | runs the Yaak secret-check tests |
| gcloud | ⛔ user installs | https://cloud.google.com/sdk/docs/install |
| Yaak | ⛔ user installs | API client — https://yaak.app/ |

## The stack

`docker-compose.yml` runs two services:

- **`postgres`** — Postgres 18 (matches the Neon major version), data in the named
  volume `qlab_pgdata`, host port 5432, gated by a `pg_isready` healthcheck.
- **`api`** — the Go service built from `backend/Dockerfile`, on host port 8090.
  Waits for Postgres to be healthy, then connects + pings it on boot (a failed
  connection is a hard boot failure, not a stream of failing requests).

Config comes from `.env.json` (gitignored; `mage` creates it from
`.env.example.json` on first run) — fields: `postgres_user`, `postgres_password`,
`postgres_db`, `postgres_port`. `mage` is the single reader: it injects these into
`docker compose`'s environment for interpolation, so drive the stack through
`mage`, not raw `docker compose`. Two database URLs exist by design: the **`api`
container** reaches Postgres at host `postgres` (built in compose), while **host
tooling** (goose via `mage migrate`) uses a `localhost` URL `mage` derives from the
same fields.

## mage targets

| Target | Does |
|--------|------|
| `mage startStack` | build + start API + Postgres (waits for Postgres health); creates `.env.json` if missing |
| `mage stopStack` | stop and remove containers; **keeps** the data volume |
| `mage resetStack` | wipe the data volume **and** recreate (fresh DB) |
| `mage migrate` | apply `goose` migrations (`backend/migrations/`) to local Postgres |
| `mage seed` | load demo data (lands with the schema in Phase 4) |
| `mage test` | run all test tiers (currently `testUnit` + `testSecurity`; integration/database tiers added later) |
| `mage testUnit` | `go test -tags testunit ./backend/...` (Go unit tests) |
| `mage testSecurity` | the Yaak secret-scanner's own tests **and** the scanner against the committed workspace |
| `mage serviceLogs` | follow all services' logs (last 100 lines, then live) |
| `mage postgresLogs` | dump Postgres's full log, then stream (debugging the DB) |
| `mage genProto` | `buf generate` (Go + TS from `.proto`; buf config lands in Phase 5) |

`migrate`/`seed`/`genProto` are wired to their real tools but skip cleanly until
the content they drive exists (migrations in Phase 4, proto in Phase 5). Unit tests
carry a `//go:build testunit` tag; other test tiers (integration, database) get
their own tags and `mage` targets as they land, each folded into `mage test`.

## Health checks

- `curl localhost:8090/healthq` → `{"status":"ok"}` — **liveness** (process up).
  Available the instant the listener binds, *before* dependencies initialize, so a
  slow database never looks like a dead container.
- `curl localhost:8090/readyq` → **readiness** (fit for traffic):
  `{"status":"unavailable"}` (503) during startup until every dependency
  initializes, then `{"status":"ok"}` (200). It's a one-way startup transition, not
  a per-request re-check.

> **Cloud Run probe mapping (Phase 3, configured by you — cloud-side):** point the
> **startup probe at `/readyq`** (holds traffic until deps are ready; give it a
> timeout generous enough for a Neon cold start) and the **liveness probe at
> `/healthq`** (restarts only a genuinely wedged process). On boot the service
> retries a transient DB failure a few times with backoff, then exits non-zero if
> it can't connect.

## Debugging

- Logs are structured `slog`: human-readable text locally, JSON in the cloud.
  Every line carries a `request_id` (echoed in the `X-Request-Id` response header)
  so a single request's full story can be filtered out and handed to Claude as a
  self-contained slice. Follow with `mage serviceLogs`.
- Inspect the DB directly: `docker exec -it qlab-postgres-1 psql -U qlab -d qlab`.
- **Staging/prod** is the user's domain (Claude drafts, never runs cloud
  commands). Deploy setup, the CI/CD pipeline, and the `gcloud logging read`
  incantations for pulling a request's log slice live in `docs/deploy.md`.
