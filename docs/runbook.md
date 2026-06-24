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

`docker-compose.yml` runs three services:

- **`postgres`** — Postgres 18 (matches the Neon major version), data in the named
  volume `qlab_pgdata`, host port 5432, gated by a `pg_isready` healthcheck.
- **`firebase-auth`** — the Firebase Auth emulator (built from
  `docker/firebase-emulator/Dockerfile`: Node + a JRE + pinned `firebase-tools`,
  Auth-only, offline under the demo project `demo-qlab`), host port 9099, gated by a
  healthcheck. Token verification and dev-login run against it locally, so **no real
  Firebase project is needed for local dev** (decision 0008).
- **`api`** — the Go service built from `backend/Dockerfile`, on host port 8090.
  Waits for Postgres and the emulator to be healthy, then connects + pings Postgres
  on boot (a failed connection is a hard boot failure, not a stream of failing
  requests). It is configured for the emulator via `FIREBASE_AUTH_EMULATOR_HOST` and
  the demo project id.

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
| `mage seed` | load demo data (`backend/seed/seed.sql`) into local Postgres — **local only** |
| `mage test` | run all test tiers: `testUnit` + `testSecurity` + `testSchema` + `testIntegration`. **Requires the stack up** (the schema + integration tiers need Postgres) |
| `mage testUnit` | `go test -tags testunit ./backend/...` (Go unit tests) |
| `mage testSecurity` | the Yaak secret-scanner's own tests **and** the scanner against the committed workspace |
| `mage testSchema` | DB-level schema tests (constraints/triggers/seed) against a throwaway DB; **requires the stack up**. Its `TestMain` creates/migrates/seeds/drops `qlab_schema_test` |
| `mage testIntegration` | full-stack suite: boots the real server (as an RLS-bound app role) against a throwaway DB and drives it through the Connect client with **real emulator-issued tokens**; **requires the stack up** (Postgres + the Auth emulator). Its `TestMain` creates/migrates/drops `qlab_integration_test` |
| `mage genMocks` / `mage clearMocks` | (re)generate / remove the mockery mocks. Mocks are **not** committed (`.mockery.yaml`); generate before building code that imports one, then `go mod tidy` |
| `mage mutate` | mutation-test the engine with gremlins; gates on mutant coverage (config in `.gremlins.yaml`; also a soft CI job). Needs gremlins installed. |
| `mage serviceLogs` | follow all services' logs (last 100 lines, then live) |
| `mage postgresLogs` | dump Postgres's full log, then stream (debugging the DB) |
| `mage genProto` | `buf generate` from `proto/`: Go → `backend/internal/protogen`, TS → `frontend/src/protogen`. Go plugins are the module's pinned `go tool` binaries; the TS plugin is `proto/package.json` (`npm install` in `proto/` first). Commit the regenerated output. |
| `mage genSqlc` | `sqlc generate` (`sqlc.yaml`): compiles `backend/internal/store/pgstore/queries.sql` against the migration schema → `pgstore/sqlcgen`. Committed; run after changing the queries or the slots/outbox schema. CI checks drift. |
| `mage dbStringStaging` / `mage dbStringProd` | **user-run only** — print the human read-write Neon connection string from Secret Manager (for DBeaver). Claude never runs these (they invoke `gcloud`). See `docs/deploy.md`. |

`genProto` generates from the `proto/qlab/v1` contract; the committed output is
verified up to date by a CI job (`buf lint` + `buf breaking` + a generate-and-diff
check). `migrate`/`seed` now drive real content. Unit tests carry a
`//go:build testunit` tag; the `database`-tagged schema tests need Postgres. `mage
test` runs all three tiers, so it now needs the stack up. In CI each tier is its
own job (the schema job runs against a Postgres service), and the deploy pipeline
runs the same CI gate before deploying — so schema tests also gate CD.

> **`mage seed` is local by construction:** it runs `psql` *inside* the Compose
> Postgres container, which has no route to Neon — there is no way for it to seed
> staging/prod. Demo data lives only in `seed.sql`; reference data that must exist
> everywhere goes in a migration.

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

## Connect API

The data API is mounted at `/qlab.v1.QlabService/<Method>` (Connect-RPC; speaks
JSON or binary protobuf on the same path). The methods are wired to the engine +
store, and **every call is authenticated**: it needs an `Authorization: Bearer
<id-token>` header and an `X-QLab-Lab: <lab-uuid>` header (the lab the caller is
acting in). A call with no token is rejected:

    curl -s -X POST localhost:8090/qlab.v1.QlabService/ListSlots \
      -H 'Content-Type: application/json' \
      -d '{"resourcePoolId":"11111111-1111-1111-1111-111111111111"}'
    # → HTTP 401, {"code":"unauthenticated", ...}

## Operator surface (staging/local dev experience)

The **operator surface** (`qlab.dev.v1.DevService`, decision 0008) provisions demo
workspaces and mints tokens to act as their users — the staging dev loop. It is a
separate Connect service mounted only outside production, gated by the operator
secret (`X-QLab-Operator-Secret`). Locally the secret is `local-operator-secret`
(docker-compose) and the elevated DB connection is the local superuser. Drive it
with curl (JSON over Connect):

    OP=localhost:8090/qlab.dev.v1.DevService
    SECRET='X-QLab-Operator-Secret: local-operator-secret'

    # Provision a per-feature workspace: a head + 3 members, 2 resources.
    curl -s -X POST $OP/ProvisionLab -H "$SECRET" -H 'Content-Type: application/json' \
      -d '{"feature":"search","memberCount":3,"resourceCount":2}'
    # → {lab:{id,name}, pool:{...}, members:[{user:{id,email},role}], resources:[...]}

    # Mint a token to act as one of those users (use a member's user id from above).
    TOKEN=$(curl -s -X POST $OP/MintToken -H "$SECRET" -H 'Content-Type: application/json' \
      -d '{"userId":"<user-uuid>"}' | jq -r .idToken)

    # Act as them against the data API, in their lab.
    curl -s -X POST localhost:8090/qlab.v1.QlabService/ListSlots \
      -H "Authorization: Bearer $TOKEN" -H "X-QLab-Lab: <lab-uuid>" \
      -H 'Content-Type: application/json' -d '{"resourcePoolId":"<pool-uuid>"}'

Other methods: `ListLabs` (`{"feature":"..."}` filter, or `{}` for all), `GetLab`
(`{"labId":"..."}` — full state export), `TeardownLab` (`{"labId":"..."}`). A call
with a wrong/missing secret is `permission_denied`.

First login provisions invited users: minting (or a real Google login) links the
Firebase identity to the `users` row matched by the verified email. A valid token
for an un-invited email is rejected with `permission_denied` (not provisioned).

> **Staging operator setup.** The operator surface needs three env vars on the
> staging Cloud Run service, from staging's Secret Manager: `OPERATOR_SECRET` (the
> gate), `OPERATOR_DATABASE_URL` (the elevated cross-tenant connection — it reuses the
> migrator/owner secret, which bypasses RLS), and `FIREBASE_WEB_API_KEY` (for
> `MintToken`'s exchange). All MUST be absent on production — the service refuses to
> boot otherwise. The runtime SA also needs `roles/firebaseauth.admin` so
> `MintToken` can create/look-up Auth users (only enforced against real Firebase, not
> the emulator). Fully wired and smoke-tested in staging; the exact grants live in
> `docs/deploy.md`.

## Debugging

- Logs are structured `slog`: human-readable text locally, JSON in the cloud.
  Every line carries a `request_id` (echoed in the `X-Request-Id` response header)
  so a single request's full story can be filtered out and handed to Claude as a
  self-contained slice. Follow with `mage serviceLogs`.
- Inspect the DB directly: `docker exec -it qlab-postgres-1 psql -U qlab -d qlab`.
- **Staging/prod** is the user's domain (Claude drafts, never runs cloud
  commands). Deploy setup, the CI/CD pipeline, and the `gcloud logging read`
  incantations for pulling a request's log slice live in `docs/deploy.md`.
