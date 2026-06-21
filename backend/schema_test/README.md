# schema_test

Database-level tests of the schema itself: that the constraints reject bad rows,
the triggers fire, the enum types carry the expected labels, the demo seed loads
the expected values, and `pgstore` connects and queries end to end.

Run them with **`mage testSchema`** (also part of `mage test`). That target injects
the coordinates the suite needs and runs `go test -tags database ./backend/schema_test/...`.

## How it works

The files carry the `database` build tag, so ordinary `go build`/`go vet` and `mage
testUnit` skip them. `TestMain` owns a throwaway database lifecycle: it creates a
fresh DB, applies every migration with the real `goose`, loads `backend/seed/seed.sql`,
runs the suite, and drops the DB. Doing it in `TestMain` (rather than the magefile)
keeps the flow identical locally and in CI — both just need a reachable Postgres.

`mage testSchema` passes these via the environment (the suite **fails loudly** if any
is missing — running these tests outside `mage testSchema` is unsupported):

| Env var | Meaning |
|---------|---------|
| `SCHEMA_TEST_DATABASE_URL` | the throwaway DB this suite creates/owns/drops |
| `SCHEMA_TEST_MIGRATIONS_DIR` | absolute path to `backend/migrations` |
| `SCHEMA_TEST_SEED_FILE` | absolute path to `backend/seed/seed.sql` |
| `SCHEMA_TEST_GOOSE_PKG` | pinned `go run` target for goose |

The package is `schematest` (the directory is `schema_test`) — there is no `doc.go`;
this README is the package's prose. The suite is test-only, which `go build ./...`
handles fine (it builds no output for a package with only test files).

## Layout

| File | Covers |
|------|--------|
| `main_test.go` | `TestMain` lifecycle, the DB connection, the isolated `fixture`, and helpers |
| `constraints_test.go` | each constraint rejects its bad row (plus a positive control) |
| `triggers_test.go` | `updated_at` advances; the ACTIVE-pin trigger blocks/permits the right mutations |
| `values_test.go` | enum labels and demo-seed values |
| `store_test.go` | `pgstore` connects and runs a query (the Phase 5 store criterion) |
