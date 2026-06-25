# 0010 — Live schedule updates: SSE, fanned out over Postgres LISTEN/NOTIFY

**Status:** Accepted (2026-06-25)

Phase 10 PR2 makes the queue live: when one user clocks out (or any event reschedules
a pool), every other user watching that pool sees the new schedule without refreshing.
This record fixes the transport, the cross-instance fan-out, and the stream auth, since
each has a non-obvious choice with real alternatives.

## Context

The product needs server→client push of "this pool's schedule changed". All writes
already go through normal RPCs that run the engine in one transaction and return the new
`RescheduleResult`; the read (`GetSchedule`) and the proto envelope (`QueueEvent`,
Phase 6) return that same shape. So the missing pieces are only: a push channel to the
browser, and a way for a write on one server instance to reach subscribers on another
(Cloud Run can run more than one instance).

Constraints: the API and PWA are separate origins and every endpoint requires a Firebase
bearer token (decision 0001); we stay on free tiers ($0); and the database is Neon,
whose **pooled** endpoint is PgBouncer in transaction-pooling mode.

## Decision

**Transport: Server-Sent Events, not WebSockets.** Updates are one-directional
(server→client) — every write is still a normal RPC — so SSE is the right fit: plain
HTTP, auto-reconnect, far less to get wrong than a bidirectional socket. The endpoint is
`GET /v1/stream/schedule?resource_pool_id=<pool>`, a plain HTTP route (not a Connect
procedure — a stream isn't a unary call). Each frame is a `QueueEvent` rendered with
protojson, so the frontend parses it with the generated `fromJson(QueueEventSchema, …)`.
The first frame is the current snapshot (so a fresh or reconnected subscriber is
immediately consistent); then one frame per change. A `:`-comment heartbeat every 25s
keeps the connection warm through idle-timeout proxies.

**Cross-instance fan-out: Postgres LISTEN/NOTIFY.** A pool-mutating transaction emits a
`pg_notify` on the `qlab_schedule_changed` channel **inside the same transaction**
(`store.WithPool`), so the signal is delivered only on commit and never for a rollback —
the fan-out is exactly as durable as the write it announces. One listener per process
holds a dedicated connection, `LISTEN`s, and dispatches each notification to an
in-process broker; the SSE handler subscribes to the broker for its pool. A write on any
instance therefore reaches subscribers on every instance. The chosen alternative — an
in-process broker only, pinned to one Cloud Run instance — was rejected: it trades a
correctness property (works at any instance count) for a scaling cap we'd have to
remember forever, and the debugging cost of LISTEN/NOTIFY is bounded and one-time.

**The NOTIFY payload is minimal; the handler recomputes.** The payload is just
`{lab_id, pool_id, kind}` — well under Postgres's 8 KB NOTIFY limit regardless of pool
size — and the SSE handler treats it as "recompute and push", re-running `Schedule` and
emitting the fresh result. So a client never sees a stale serialized snapshot, ordering
races are impossible (the latest read wins), and the store never serializes proto into a
notification. Cost: one extra read per event per subscribed connection — negligible at
QLab's scale; revisit (coalesce per pool) only if it ever isn't.

**Stream auth reuses the bearer path, via fetch-event-source.** The stream is
authenticated exactly like every RPC: the same `resolvePrincipal` (bearer token +
`X-QLab-Lab` membership/RLS) the Connect interceptor uses. The browser's native
`EventSource` **cannot send an `Authorization` header**, so the frontend uses
`@microsoft/fetch-event-source` (a fetch-based SSE client) to send the same headers the
api transport does. Putting the token in the URL was rejected — it leaks into access
logs, history, and referrers. The frontend owns its own reconnect loop (backoff, a fresh
token minted per attempt) so the stream survives token expiry; each reconnection's
snapshot frame closes any gap.

**The listener needs a session-pinned connection (the Neon wrinkle).** LISTEN requires a
connection that stays the same session for its lifetime, which a transaction-pooled
endpoint does not provide — on Neon's **pooled** host, `LISTEN` runs but notifications
are never delivered. So the listener opens its own dedicated connection to the **direct
(unpooled)** endpoint, configured by `SCHEDULE_LISTENER_DATABASE_URL` (optional; falls
back to `DATABASE_URL`). Locally the single Postgres has no pooler, so the default is
correct and live updates work out of the box; in staging/prod this must be set to the
direct endpoint (see `docs/deploy.md`). Until it is, the app still works — queues load
and the acting user's mutation refetches — there are simply no cross-client live pushes.

## Consequences

- **Correct at any instance count**, at the cost of a dedicated listener connection per
  instance and a second (direct-endpoint) DB string in the cloud.
- **No new long-lived infra**: no message broker, no WebSocket gateway; LISTEN/NOTIFY is
  built into the database we already run.
- **Degrades safely**: a listener that can't connect (or an unset direct endpoint) means
  "no live updates", not an outage — the process logs and keeps retrying; clients load
  via `GetSchedule` and reconnect their streams.
- **One data model end to end**: snapshot, mutation response, and stream frame are all
  `RescheduleResult`, so the frontend keeps a single cache (the `GetSchedule` query) and
  the stream just writes into it — no normalization, no optimistic state.
- **Cloud Run** must allow long-lived requests: the deploy sets `--timeout=3600`; SSE
  runs over HTTP/1.1 and the client reconnects across the cap.
- The cross-instance path and the Neon-direct requirement are unverifiable locally (one
  instance, no pooler); they are covered by the integration suite end-to-end against a
  single Postgres and confirmed in staging when the direct endpoint is wired.

## Alternatives considered

- **WebSockets** — bidirectional, heavier, needs its own framing/reconnect/auth; no
  benefit when all writes are RPCs. Rejected.
- **In-process broker, pinned to one instance** — simplest, but caps scaling and the cap
  is an invisible foot-gun. Rejected in favour of LISTEN/NOTIFY (the interface is the
  same `Broker`, so this remains a fallback if Neon LISTEN ever proves troublesome).
- **Carry the full result in the NOTIFY payload** — hits the 8 KB limit on large pools
  and reintroduces ordering/staleness; recompute-on-signal avoids both. Rejected.
- **Native `EventSource` + token in the query string** — no dependency, but leaks a
  credential into URLs. Rejected for fetch-event-source's header auth.
- **A short-poll of `GetSchedule`** — trivial, but wasteful and laggy; SSE is barely more
  code and is genuinely live. Rejected.
