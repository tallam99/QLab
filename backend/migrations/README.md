# migrations

Goose migrations for Postgres (local + Neon). Applied locally via `mage migrate`,
which runs goose `up` against the Compose Postgres. Each file is reversible
(`-- +goose Up` / `-- +goose Down`); `mage testSchema` exercises a full
migrate-up against a throwaway database on every run.

## Schema (Phase 5)

| File | Adds |
|------|------|
| `00001_extensions_and_enums.sql` | `btree_gist`; enums `lab_role`, `resource_kind`, `slot_status`, `outbox_status` |
| `00002_core_identities.sql` | `labs`, `users`, `lab_memberships` |
| `00003_resources.sql` | `resource_pools`, `resources` (equipment as a pool of interchangeable resources) |
| `00004_slots.sql` | `slots` (the priority queue) + the `slot_occupancy()` helper + no-overlap constraint |
| `00005_outbox.sql` | `outbox` (transactional notification outbox; drained in Phase 11) |
| `00006_triggers.sql` | `updated_at` touch trigger; the ACTIVE-pin immutability trigger |

The tables mirror the pure engine's domain types (`internal/dynamicqueue`,
ALGORITHM §1). `slot_status` is intentionally broader than the engine's input
states: the engine sees only `SCHEDULED`/`ACTIVE` (history filtered out, `NO_SHOW`
is an output), while the row tracks the full lifecycle.

## The database enforces the domain (belt-and-suspenders)

Beyond columns, these migrations make domain-invalid rows unrepresentable —
composite FKs (cross-lab / cross-pool / kind consistency, member-only booking),
CHECKs (`lookahead >= 0`, `duration > 0`, ACTIVE-pinned), a partial unique index
for the live `slot_priority` total order, a GiST exclusion constraint for
per-resource no-overlap, and triggers for ACTIVE immutability. See **decision
0003** for the rationale and `backend/schema_test/` for the suite that proves each
one fires.

## Cloud (staging/prod)

Migrations are **not** run against Neon from a local machine — by neither the user
nor Claude (see `docs/deploy.md`). Demo/seed data is **never** added here; it lives
in `backend/seed/seed.sql` and is local-only via `mage seed`. Anything that must
exist in staging/prod (reference data) would go in a regular migration, but the MVP
has none (kinds are enums, not lookup tables).
