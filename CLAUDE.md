# QLab — orientation for Claude

QLab is a lab-equipment scheduling PWA. Its differentiator is a **multi-bench
scheduling engine** that continuously re-flows a priority queue across
interchangeable benches as experiments overrun, finish early, or get cancelled.

**Current status:** Phase 2 (local dev stack). The Go service and a one-command
local stack (Docker Compose + Postgres, `mage` targets) are in place; the data
model, API, and engine are next. Work proceeds through the phases in `docs/PLAN.md`.

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

- **Task runner:** `mage` (`magefile.go` at the repo root). Targets: `up`, `down`,
  `reset`, `migrate`, `seed`, `test`, `logs`, `proto`. See `docs/runbook.md`.
- **Wire format:** Protobuf via Connect-RPC + buf. `.proto` is the contract of record;
  Go + TS types are generated — don't hand-write request/response shapes.
- **Logging:** `slog` (structured, JSON in cloud). **Tracing:** OpenTelemetry → Cloud
  Trace; annotate spans with `lab_id`, `pool_id`, `slot_id`, event type.
- **Topology:** the public PWA (Firebase Hosting) and the data API (Cloud Run) are
  **separate origins**; every API endpoint requires a Firebase JWT. See decision 0001.
- **Multi-tenancy:** every tenant-scoped row carries `lab_id`; scope every query by it.
- **Docs:** live in `docs/`; root `README.md` is the entry point; subfolders may have
  their own `docs/` and `CLAUDE.md`. Update docs as part of every phase.
- **Cost:** stay within free tiers ($0/month).

## Frameworks

See `docs/PLAN.md` → "Frameworks & libraries" for the confirmed stack — Go: chi, pgx,
sqlc, squirrel, goose, testify, protovalidate, envconfig; Frontend: Vite, Vitest, RTL,
Playwright, Connect-Query, Tailwind, Biome, vite-plugin-pwa.

## Planned repo layout

    backend/    Go API + scheduling engine (internal/scheduling is pure: no DB/HTTP/clock)
    frontend/   React PWA
    proto/      .proto contract (buf)
    docs/       project docs

Not all of these exist yet — they're scaffolded in Phase 1 (per-area `CLAUDE.md`
files land with their directories then).
