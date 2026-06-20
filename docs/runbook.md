# QLab — Local Runbook

> How to run and debug the **local** stack. Claude operates this environment
> autonomously (see `CLAUDE.md`); the user drives staging/prod.
>
> **Status:** the local stack (Docker Compose + Postgres, `mage` targets) is built in
> Phase 2. Until then this is a placeholder describing the intended commands.

## Prerequisites

| Tool | Status | Notes |
|------|--------|-------|
| Go | ✅ installed | 1.26+ |
| Node.js | ✅ installed | current LTS |
| Docker Desktop | ✅ installed | for Compose + local Postgres |
| buf | ✅ installed | in `C:\Users\thfif\go\bin` (on PATH) |
| mage | ✅ installed | task runner |
| gcloud | ⛔ user installs | https://cloud.google.com/sdk/docs/install |
| Yaak | ⛔ user installs | API client — https://yaak.app/ |

## mage targets (planned — Phase 2)

| Target | Does |
|--------|------|
| `mage up` | bring up the Go API + local Postgres (Docker Compose) |
| `mage down` | tear the stack down |
| `mage reset` | wipe + recreate (fresh DB) |
| `mage migrate` | run goose migrations against local Postgres |
| `mage seed` | load demo labs/users/bench-pools/queues (local/staging only) |
| `mage test` | run the full test suite |
| `mage logs` | tail service logs |
| `mage proto` | `buf generate` (Go + TS from `.proto`) |

## Debugging (planned)

- Structured `slog` JSON logs; filter by request/trace id to hand Claude a
  self-contained slice.
- Staging log queries: `gcloud logging read …` incantations will live here.

_Fill in concrete commands as Phase 2 lands them._
