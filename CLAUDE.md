# QLab — orientation for Claude

QLab is a lab-equipment scheduling PWA. Its differentiator is a **multi-resource
scheduling engine** that continuously re-flows a priority queue across
interchangeable resources as experiments overrun, finish early, or get cancelled.

**Current status:** Phase 7 (API endpoints). The Go service, a one-command local
stack (Docker Compose + Postgres, `mage` targets), the GitHub Actions pipeline (CI
gate + deploy to Cloud Run + Firebase Hosting, both environments — see
`docs/deploy.md`), the pure scheduling engine (`internal/dynamicqueue`, Phase 4),
the data model (goose migrations + a self-enforcing schema, seed, and the
`schema_test` suite — Phase 5), the proto contract (`proto/qlab/v1`, generated Go +
TS, Phase 6), and now the **real, persisted Connect API** — the eight RPCs wired
through a domain scheduling service (`internal/scheduling`) to the engine + store in
one transaction per event, with a full-stack integration suite
(`backend/integration_test`, `mage testIntegration`) — are in place. Auth (real
Firebase JWTs) is next (Phase 8); Phase 7 uses a dev-header principal stand-in. Work
proceeds through the phases in `docs/PLAN.md`.

## Read these first

- **`docs/PLAN.md`** — the phased roadmap. Find the current phase; don't skip ahead.
- **`docs/ALGORITHM.md`** — the scheduling-engine spec. **This is the heart of the
  product.** Read it in full before writing or changing any scheduling logic. It is
  the schema-of-record: code implements it, not the other way around.
- **`docs/ARCHITECTURE.md`** — the system map.
- **`docs/decisions/`** — why non-obvious cross-cutting choices were made.

## How work is split (important)

This project is built with Claude as the primary engine, with a hard boundary:

- **Local** (Docker Compose, local Postgres, migrations, seed, tests, `buf`, Yaak):
  **Claude operates autonomously** — spin up/tear down, migrate, seed/wipe, run the
  suite, exercise the API, read logs. No human in the loop.
- **Staging / Production** (Cloud Run, Neon branches, Firebase): **the user drives.**
  Claude prepares commands/PRs and reads artifacts the user provides, but never
  deploys, migrates, or mutates staging/prod.
- **Cloud-provider CLIs/consoles** (`gcloud`, `neonctl`, Firebase staging/prod):
  Claude must **not authenticate to or invoke them at all — not even read-only
  listing/verification.** The user runs all cloud auth and commands; Claude only
  drafts them. (Installing a cloud SDK binary locally is fine; *using* it is not.)

## Conventions

- **Task runner:** `mage` (`magefile.go` at the repo root). Targets: `startStack`,
  `stopStack`, `resetStack`, `migrate`, `seed`, `testUnit`, `serviceLogs`,
  `postgresLogs`, `genProto`. See `docs/runbook.md`.
- **Wire format:** Protobuf via Connect-RPC + buf. `.proto` is the contract of record;
  Go + TS types are generated — don't hand-write request/response shapes.
- **Logging:** `slog` (structured, JSON in cloud). **Tracing:** OpenTelemetry → Cloud
  Trace; annotate spans with `lab_id`, `resource_pool_id`, `slot_id`, event type.
- **Topology:** the public PWA (Firebase Hosting) and the data API (Cloud Run) are
  **separate origins**; every API endpoint requires a Firebase JWT. See decision 0001.
- **Multi-tenancy:** every tenant-scoped row carries `labs_id`; scope every query by
  it. Row-level security enforces the same isolation at the DB (decision 0005): the
  service sets `app.current_lab_id` per request and the app's DB role is RLS-bound.
- **Docs:** live in `docs/`; root `README.md` is the entry point; subfolders may have
  their own `docs/` and `CLAUDE.md`. Update docs as part of every phase.
- **Cost:** stay within free tiers ($0/month).

## Frameworks

See `docs/PLAN.md` → "Frameworks & libraries" for the confirmed stack — Go: chi, pgx,
sqlc, squirrel, goose, testify, protovalidate, envconfig; Frontend: Vite, Vitest, RTL,
Playwright, Connect-Query, Tailwind, Biome, vite-plugin-pwa.

## Repo layout

The repo is a **single Go module** rooted at the top (`github.com/tallam99/qlab`);
the `magefile.go` shares it, and `backend/` is a subtree, not a separate module.

    backend/    Go API + scheduling engine (internal/dynamicqueue is pure: no DB/HTTP/clock)
    frontend/   React PWA (scaffolded in a later phase)
    proto/      .proto contract (buf; generated Go → backend/internal/protogen, TS → frontend/src/protogen)
    docs/       project docs

Per-area `CLAUDE.md` files live with their directories.
