# migrations

Goose migrations for Postgres (local + Neon). Applied locally via `mage migrate`,
which runs goose `up` against the Compose Postgres. In CI/CD they run against the
target Neon branch **before** each deploy (see `docs/deploy.md`). Each file is
reversible (`-- +goose Up` / `-- +goose Down`); `mage testSchema` exercises a full
migrate-up against a throwaway database on every run.

## Schema (Phase 5)

| File | Adds |
|------|------|
| `00001_extensions_and_enums.sql` | `btree_gist`; enums `lab_role`, `resource_kind`, `slot_status`, `outbox_status` |
| `00002_core_identities.sql` | `users`, `labs`, `labs_users` (the membership join) |
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

## Conventions

- **File names:** `NNNNN_snake_case.sql`, zero-padded and strictly increasing;
  goose applies them in that order and records each in `goose_db_version`.
- **Every migration is reversible:** an `-- +goose Up` and a matching `-- +goose
  Down`. The Down reverses exactly what the Up created, in reverse dependency order.
- **One concern per migration**, in dependency order within the file (a table's
  referenced unique constraints exist before the FK that targets them).
- **Plain `CREATE`, not `CREATE IF NOT EXISTS` / `CREATE OR REPLACE`**, for objects
  a migration owns. goose applies each migration exactly once, so idempotency
  guards aren't needed and would only mask drift (a re-create that should error if
  the object already exists). The **exception is `CREATE EXTENSION IF NOT EXISTS`**:
  extensions are environment-level and may already be enabled (e.g. on Neon).
- **Wrap function/`DO` bodies** (anything with embedded `;`) in `-- +goose
  StatementBegin` / `-- +goose StatementEnd` so goose doesn't split them.
- **Primary keys are `<table>_id`** (`labs_id`, `users_id`, …) and foreign keys
  reuse that exact name, so joins can `USING (labs_id)` without `.id` prefixing.
- **Join tables are `table1_table2`** (alphabetical-ish, plural), e.g. `labs_users`.
- **Audit columns on every table:** `created_at` / `updated_at` (timestamptz,
  defaulted; `updated_at` maintained by the `set_updated_at` trigger) and
  `created_by` / `updated_by` (uuid → `users(users_id)`, nullable for
  system/seed-origin rows; **application-set** — the DB can't know the acting user).
- **Native enums** for closed sets, with labels matching the Go `enumer` strings.
- **Times** are `timestamptz`; `duration` / `lookahead` are integer minutes.

## Cloud (staging/prod)

Migrations run against Neon **only from CI/CD** (the deploy pipeline, before each
Cloud Run deploy), never from a laptop — by neither the user nor Claude (see
`docs/deploy.md`). Demo/seed data is **never** added here; it lives in
`backend/seed/seed.sql` and is local-only via `mage seed`. Reference data that must
exist everywhere would go in a regular migration, but the MVP has none (kinds are
enums, not lookup tables).
