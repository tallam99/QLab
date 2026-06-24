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

- **API / Connect handlers** (`internal/api`) — thin transport adapters; convert
  proto ⇄ domain at the edges and call the scheduling service.
- **Domain services** (`internal/services/*`, each an `interface.go` + `v1/` impl):
  - **scheduling** — the orchestration between the API and the engine, store,
    authorizer, and notification builder (all interfaces). One method per ALGORITHM
    §6 event; each mutating one runs in a single transaction (load the pool's live
    slots `FOR UPDATE` → run the engine → persist placements → enqueue outbox), and
    owns the next-in-line reclaim rule.
  - **authz** — the authorization policy (lab membership now, roles in Phase 8),
    reading through the store (no separate database).
  - **notifications** — builds the outbox row for each event (re-commit, poke);
    delivery is Phase 11.
  - **authentication** — resolves a verified bearer token to a local `users` row,
    provisioning invited users on first login by linking their Firebase uid to the
    row matched by verified email (invite-only; decision 0007).
- **Scheduling engine** (`internal/dynamicqueue`) — **pure**, no DB/HTTP/clock.
  The product's core; specified in `docs/ALGORITHM.md`. A single `reschedule()`
  operation re-flows the queue on every event.
- **Persistence** — Postgres via pgx; queries via sqlc (static) + squirrel (dynamic);
  migrations via goose. Each mutating event runs in one transaction (`FOR UPDATE` on
  the pool's slots) so the queue is never observed half-shifted.
- **Notifications** — `Notifier`/`Channel` abstraction + transactional outbox with
  retry and dead-letter; email first, SMS/push additive.
- **Auth** — a Connect interceptor verifies the Firebase ID token (via the
  `auth.TokenVerifier` seam over the Admin SDK; the Auth emulator locally), resolves
  it to a user through the `authentication` service, and attaches the principal with
  the selected lab. A bad token is `Unauthenticated`, a valid-but-uninvited one
  `PermissionDenied` (decision 0007).
- **Operator surface** (`internal/devapi` over `services/operator`) — a
  **separate** Connect service (`qlab.dev.v1`) for the staging dev experience:
  provision demo workspaces, mint a token to act as any seeded user, list/inspect/
  tear down workspaces. Gated by an operator secret and run over an elevated
  (RLS-bypassing) DB connection. Mounted only outside production — the prod binary
  contains no operator capability at all (decision 0008).

## Data model (shape)

Tenant-scoped by `labs_id`. Core tables: `users`, `labs`, `labs_users` (membership
+ role), equipment as **resource pools** + interchangeable **resources** (each a
`kind`), `slots` (priority queue with `slot_priority` / `desired_start` /
`lookahead` / `duration` / `committed_start` / `actual_start` / `resources_id` /
`status`), `outbox`. Every table carries audit columns (`created_at`/`updated_at`,
`created_by`/`updated_by`); ids follow `<table>_id`. See `docs/PLAN.md` Phase 5 and
`docs/ALGORITHM.md` §1.

The **database enforces the domain itself** (decision 0003): native enums,
composite FKs (cross-lab / pool / kind consistency, member-only booking), CHECKs,
a partial-unique live `slot_priority` order, a GiST per-resource no-overlap
exclusion constraint, and triggers (ACTIVE immutability, `updated_at`).
**Tenant isolation is also enforced by row-level security** (decision 0005) behind
the app's `labs_id` scoping: lab-scoped tables expose only the lab in the session's
`app.current_lab_id` (set per request), fail-closed. The app connects as a
least-privilege, non-`BYPASSRLS` role, never the Neon owner (decision 0004). The
schema lives in `backend/migrations` (goose), is applied to Neon by CI before each
deploy, and is regression-tested by `backend/schema_test` (`mage testSchema`).

## Live updates

Server-Sent Events (SSE) push queue-changed events (proto envelope) to subscribed
clients; all writes still go through normal API calls. Chosen over WebSockets for
simplicity (one-directional, plain HTTP, auto-reconnect).

## Environments

| Env | Frontend | API | DB | Auth |
|-----|----------|-----|----|----|
| local | Vite dev server | Go in Docker Compose | local Postgres | Auth emulator + operator surface |
| staging | Firebase Hosting (staging) | Cloud Run (staging) | Neon staging branch | Firebase staging |
| prod | Firebase Hosting (prod) | Cloud Run (prod) | Neon prod branch | Firebase prod |

Claude operates **local** only; the user drives staging/prod (see `CLAUDE.md`).

## Cross-cutting

- **Contract:** protobuf via Connect + buf — one schema, generated Go + TS.
- **Observability:** `slog` JSON + OpenTelemetry spans (→ Cloud Trace), keyed by
  `lab_id` / `resource_pool_id` / `slot_id` for selectively-feedable debugging.
- **Scaling note:** the engine's cost is **per-pool** (bounded by one lab's queue);
  scaling to many labs is horizontal/infra (DB connections, SSE capacity), not
  algorithmic. The engine is only revisited if a *single pool's* queue grows large.
