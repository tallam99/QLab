# QLab — Architecture

> The system map. High-level and evolving; fleshed out as phases land. For the build
> order see `docs/PLAN.md`; for the scheduling engine see `docs/ALGORITHM.md`.

## Overview

QLab is two separate surfaces plus managed backing services:

    ┌─────────────────────────┐
    │  React PWA (static)     │   public surface
    │  Firebase Hosting       │   qlab.app
    └───────────┬─────────────┘
                │  HTTPS + Firebase JWT (CORS)
    ┌───────────▼─────────────┐
    │  Connect-RPC API (Go)   │   data surface — auth on every call
    │  Google Cloud Run       │   api.qlab.app
    └───┬─────────┬───────┬───┘
        │         │       │
    ┌───▼────┐ ┌──▼─────┐ ┌▼────────────┐
    │  Neon  │ │Firebase│ │ Email        │
    │Postgres│ │  Auth  │ │ (Resend/SG)  │
    └────────┘ └────────┘ └──────────────┘

- **Public surface** — the PWA: static HTML/JS/CSS on a CDN. No data, no secrets.
- **Data surface** — the Connect API: every endpoint requires a verified Firebase JWT
  and is scoped to the caller's lab. No public/marketing content. (Decision 0001.)

## Backend components (Go)

- **API / Connect handlers** — thin; convert proto ⇄ domain at the edges.
- **Scheduling engine** (`internal/scheduling`) — **pure functions**, no DB/HTTP/clock.
  The product's core; specified in `docs/ALGORITHM.md`. A single `reschedule()`
  operation re-flows the queue on every event.
- **Persistence** — Postgres via pgx; queries via sqlc (static) + squirrel (dynamic);
  migrations via goose. Each mutating event runs in one transaction (`FOR UPDATE` on
  the pool's slots) so the queue is never observed half-shifted.
- **Notifications** — `Notifier`/`Channel` abstraction + transactional outbox with
  retry and dead-letter; email first, SMS/push additive.
- **Auth middleware** — verifies Firebase JWTs, maps UID → user → lab membership/role.

## Data model (shape)

Tenant-scoped by `lab_id`. Core tables: `labs`, `users`, `lab_memberships` (role),
equipment as **bench pools**, `slots` (priority queue with `win_start` / `window` /
`duration` / `actual_start` / `assigned_bench_id` / `status`), `outbox`. See
`docs/PLAN.md` Phase 4 and `docs/ALGORITHM.md` §1.

## Live updates

Server-Sent Events (SSE) push queue-changed events (proto envelope) to subscribed
clients; all writes still go through normal API calls. Chosen over WebSockets for
simplicity (one-directional, plain HTTP, auto-reconnect).

## Environments

| Env | Frontend | API | DB | Auth |
|-----|----------|-----|----|----|
| local | Vite dev server | Go in Docker Compose | local Postgres | dev-login |
| staging | Firebase Hosting (staging) | Cloud Run (staging) | Neon staging branch | Firebase staging |
| prod | Firebase Hosting (prod) | Cloud Run (prod) | Neon prod branch | Firebase prod |

Claude operates **local** only; the user drives staging/prod (see `CLAUDE.md`).

## Cross-cutting

- **Contract:** protobuf via Connect + buf — one schema, generated Go + TS.
- **Observability:** `slog` JSON + OpenTelemetry spans (→ Cloud Trace), keyed by
  `lab_id` / `pool_id` / `slot_id` for selectively-feedable debugging.
- **Scaling note:** the engine's cost is **per-pool** (bounded by one lab's queue);
  scaling to many labs is horizontal/infra (DB connections, SSE capacity), not
  algorithmic. The engine is only revisited if a *single pool's* queue grows large.
