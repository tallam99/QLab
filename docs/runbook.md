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
| `mage testUnit` | `go test -tags testunit ./backend/...` **and** `python3 scripts/test_check_yaak_secrets.py` |
| `mage serviceLogs` | follow service logs (last 100 lines, then live) |
| `mage genProto` | `buf generate` (Go + TS from `.proto`; buf config lands in Phase 5) |

`migrate`/`seed`/`genProto` are wired to their real tools but skip cleanly until
the content they drive exists (migrations in Phase 4, proto in Phase 5). Unit tests
carry a `//go:build testunit` tag; other test tiers (integration, database) get
their own tags and targets as they land.

## Health checks

- `curl localhost:8090/healthz` → `{"status":"ok"}` — **liveness** (process up).
- `curl localhost:8090/readyz` → `{"status":"ok"}` (200) when the DB is reachable,
  `{"status":"unavailable"}` (503) otherwise — **readiness** (fit for traffic).

## Debugging

- Logs are structured `slog`: human-readable text locally, JSON in the cloud.
  Every line carries a `request_id` (echoed in the `X-Request-Id` response header)
  so a single request's full story can be filtered out and handed to Claude as a
  self-contained slice. Follow with `mage serviceLogs`.
- Inspect the DB directly: `docker exec -it qlab-postgres-1 psql -U qlab -d qlab`.
- Staging log queries (`gcloud logging read …`) will live here once Phase 3 lands.
