# 0003 — The database enforces domain invariants (belt-and-suspenders)

**Status:** Accepted (2026-06-21)

## Context

The scheduling domain has rules that are *fundamentally* true regardless of which
code path writes a row: a slot can't belong to one lab while its resource pool
belongs to another; a resource can't be a different `kind` than its pool; two live
slots can't occupy the same resource at the same time; a running (`ACTIVE`) slot
can't have its resource or start time changed. The service already enforces these,
but the service is one of several things that can touch the database (migrations,
ad-hoc fixes via DBeaver, a future second writer, a bug). We want these
invariants to hold even when the service doesn't run the write.

## Decision

Make domain-invalid rows **unrepresentable in the schema**, not merely rejected by
application code. Concretely, in the Phase 5 migrations:

- **Native PG `ENUM` types** for `lab_role`, `resource_kind`, `slot_status`,
  `outbox_status` (labels match the Go `enumer` strings).
- **Composite foreign keys** carrying redundant columns so cross-row consistency is
  structural: `resources`→`resource_pools` on both `(id, lab_id)` and `(id, kind)`;
  `slots`→`resource_pools` on `(id, lab_id)`; `slots`→`resources` on
  `(id, resource_pool_id)`; `slots`→`lab_memberships` on `(lab_id, user_id)` (the
  booker must be a member of the lab).
- **CHECKs**: `lookahead >= 0`, `duration > 0`, lowercase email, and "an `ACTIVE`
  slot has a resource and an actual start."
- **A partial unique index** on `(resource_pool_id, slot_priority)` for live slots —
  `slot_priority` is a unique total order across the open queue (ALGORITHM §1.3/§4).
- **A GiST exclusion constraint** (`btree_gist`) enforcing per-resource no-overlap
  (invariant #1), `DEFERRABLE INITIALLY DEFERRED` so a single reschedule transaction
  may pass through transient overlaps and is checked only at COMMIT.
- **Triggers** for what constraints can't express: `updated_at` maintenance, and
  "ACTIVE is untouched" (a running slot's resource/start are immutable and it may
  only settle to COMPLETE/CANCELLED — invariant #5).

A `database`-tagged test suite (`backend/schema_test`, run via `mage testSchema`)
asserts each of these rejects the bad row / fires the trigger, so the enforcement
is itself regression-tested.

## Consequences

- The same rule is stated twice (service + DB). That duplication is the point —
  it's defense in depth, and the DB copy is the one that can't be bypassed.
- The composite FKs require extra `UNIQUE` constraints purely as FK targets; minor
  schema noise, documented inline.
- The `timestamptz + interval` used by the exclusion range is only `STABLE`, so it's
  wrapped in an `IMMUTABLE` `slot_occupancy()` function — correct because our
  intervals are minutes-only (pure UTC addition).
- **Performance:** all of this is negligible at QLab's scale (a handful of resources,
  a short per-pool queue); the GiST index is tiny and checks are microseconds.
  Revisit only if a single pool's queue ever grows large (ALGORITHM §5.2).
- The `resource.kind = pool.kind` FK can't be exercised by a test until a second
  `resource_kind` exists (MVP ships only `VENT_HOOD`); noted in the suite.

## Alternatives considered

- **Application-only enforcement.** Less schema, but any non-service writer (a manual
  DBeaver fix, a future job, a bug) could persist a corrupt queue. Rejected — the
  engine's correctness depends on these invariants holding for *every* writer.
- **`text` + `CHECK (… IN (…))` instead of native enums.** Slightly more flexible to
  change, but less self-documenting and no stronger. Native enums chosen for clarity.
- **No-overlap left to the engine + tests only (skip the GiST constraint).** Simpler
  schema, but drops the one structural guard against a corrupt timeline. Rejected
  because the cost is nil at our scale and the guarantee is load-bearing.
