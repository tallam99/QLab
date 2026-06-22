# 0005 — Row-level security for tenant isolation

**Status:** Accepted (2026-06-21)

## Context

QLab is multi-tenant: every lab-scoped row carries `labs_id`, and the convention is
"scope every query by `labs_id`." That scoping lives in application code, so a
missed `WHERE labs_id = …` — a bug, a new query, a hand-written admin statement —
could leak or cross-write another lab's data. The project already prefers
belt-and-suspenders enforcement at the database (decision 0003); tenant isolation
is the highest-stakes thing to back up that way.

A related concern surfaced in review: the schema tests connected as a Postgres
superuser, which bypasses RLS — so any policy we add could be silently untested.

## Decision

Enforce tenant isolation in the database with **row-level security** on the
lab-scoped tables (`labs`, `labs_users`, `resource_pools`, `resources`, `slots`,
`outbox`), as defense-in-depth behind the app's own scoping. Each table gets one
`FOR ALL` policy:

```sql
USING      (labs_id = current_setting('app.current_lab_id', true)::uuid)
WITH CHECK (labs_id = current_setting('app.current_lab_id', true)::uuid)
```

- The service sets `app.current_lab_id` per request transaction (Phase 7). The
  `true` (missing-ok) makes an unset context resolve to `NULL`, so `labs_id = NULL`
  matches no rows and permits no writes — **fail-closed**.
- The app connects as **`qlab_app`** (non-owner, `NOBYPASSRLS`), so it is bound by
  the policies. The owner (migrations) and superusers bypass RLS, so migrations and
  seeding are unaffected.
- **`users` is intentionally not covered:** a user isn't lab-scoped (one person can
  belong to several labs). The service scopes user reads via membership; cross-lab
  user-row exposure to the app role is accepted for now (flagged).

The schema tests now create a dedicated **non-privileged role** and run the RLS
cases as it (the superuser cases still cover constraints/triggers, which RLS
doesn't affect), so the policies are actually exercised.

## Consequences

- A missing app-side `labs_id` filter no longer leaks across tenants — the DB
  refuses the rows. The app **must** set `app.current_lab_id`, or it sees nothing
  (a loud, safe failure rather than a silent leak).
- Human roles (`qlab_human_*`) are also `NOBYPASSRLS`, so ad-hoc DBeaver inspection
  must `set_config('app.current_lab_id', …)` per session (or be granted `BYPASSRLS`
  for cross-lab visibility — operator's choice; documented in `docs/deploy.md`).
- A small per-query overhead from the policy predicate; negligible at QLab's scale.
- RLS is no substitute for the app scoping — it's the second layer. Both stay.

## Alternatives considered

- **App-only scoping** (the prior state). One layer; a single missed filter leaks a
  tenant. Rejected — tenant isolation is too important to rest on query discipline.
- **A separate database/schema per lab.** Strongest isolation, but heavy operational
  cost (migrations × N, connection sprawl) and at odds with the $0 / single-Neon
  model. Overkill for a 15-person-lab product.
- **`FORCE ROW LEVEL SECURITY`** (apply RLS even to the table owner). Would break
  owner-run migrations/seeding for no real gain, since the owner credential is
  trusted and CI-only. Not used.
