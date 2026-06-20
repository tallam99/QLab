# migrations

Goose migrations for Postgres (local + Neon). Applied via `mage migrate`, which
runs `go tool goose -dir migrations postgres "$DATABASE_URL" up`.

Empty for now — the schema (`labs`, `users`, `lab_memberships`, bench pools,
queues, …) lands in Phase 5 (`docs/PLAN.md`). The directory exists so `mage
migrate` runs cleanly today: it connects, ensures the goose version table, and
finds nothing to apply.
