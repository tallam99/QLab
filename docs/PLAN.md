# QLab — Build Plan

> Lab equipment scheduling PWA with a live queue + multi-bench scheduling engine.
> See **`docs/ALGORITHM.md`** for the
> scheduling-engine specification (written up front, before any engine code). This
> file is the *engineering* roadmap: broad phases, ordered, with exit criteria.

## How to read this plan

Read the **Guiding constraints** and **Cross-cutting decisions & conventions**
sections first — they apply to every phase and are referenced throughout. Then
the phases. Each phase has:
- **Goal** — the one-sentence outcome.
- **Work** — the concrete steps.
- **Exit criteria** — how you know you're done and can move on.
- **Notes** — gotchas, and explanations of unfamiliar tooling.

Don't skip ahead. Each phase deliberately ends in something *deployed and
verifiable*, so that when something breaks later you know which layer to blame.

---

## Guiding constraints

These shape every decision below. If a phase's approach ever conflicts with one
of these, stop and flag it.

### 1. Zero running cost
The entire stack must run for **$0/month aside from the Anthropic subscription.**
Every service chosen (Cloud Run, Neon, Firebase Auth + Hosting, Resend/SendGrid,
GitHub Actions, Cloud Scheduler, GCP Secret Manager) has a free tier that
comfortably covers a 15-person lab. If a phase needs a paid feature, treat that as
a decision to escalate, not a default to accept.

### 2. Claude operates local; you operate staging/prod
This project is built to be driven with Claude as the primary engine, so the
boundary of what Claude may touch is explicit:

| Environment | Who drives it | What's allowed |
|-------------|---------------|----------------|
| **Local** (Docker Compose, local Postgres, migrations, seed data, tests, `buf`, Yaak) | **Claude, autonomously** | Spin up/tear down infra, run migrations, seed/wipe data, run the full test suite, exercise the API, read all logs. No human in the loop needed. |
| **Staging** (Cloud Run staging, Neon staging branch, Firebase staging) | **You**, Claude assists | Claude prepares commands/PRs and reads exported logs/data you provide; **you** run deploys and anything that touches the live staging environment. |
| **Production** (Cloud Run prod, Neon prod branch, Firebase prod) | **You only** | Claude never deploys, migrates, or mutates prod. Claude may draft the change and read redacted artifacts you hand it. |

The practical implication: **local infra must be fully scriptable and
self-contained** (a **[mage](https://magefile.org/)** target set Claude can invoke
— `up`, `down`, `migrate`, `seed`, `reset`, `test`, `logs`, `proto`), so Claude can
reproduce and debug any behavior without you in the loop. This is a hard
requirement on Phase 2 and Phase 4, not a nicety.

### 3. Documentation-first, for fast Claude bootstrap
You must be able to open a fresh Claude session *anywhere* in the codebase and get
it productive quickly. **Project docs live in a top-level `docs/` folder**, with
`README.md` at the repo root as the entry point; any subfolder that warrants its
own area docs gets its own `docs/` folder too.
- **`README.md`** (repo root) — what/why, quickstart, and pointers into `docs/`.
- **`docs/`** — project-wide docs: **`docs/ARCHITECTURE.md`** (the map),
  **`docs/ALGORITHM.md`** (the engine), **`docs/PLAN.md`** (this roadmap),
  **`docs/runbook.md`** (how to run/debug local infra — the same commands Claude
  uses), and a **`docs/decisions/`** decision log capturing the "why" behind choices
  that aren't obvious from code (e.g. the `window` semantics, the bench-pool model,
  the topology split).
- A root **`CLAUDE.md`** plus **per-area `CLAUDE.md`** files (`backend/`,
  `frontend/`, `proto/`) pointing Claude at each area's conventions and key files; a
  subfolder needing more than a `CLAUDE.md` gets its own `docs/` (e.g.
  `backend/docs/`).
- Every phase's exit criteria includes *"docs updated."*

---

## Cross-cutting decisions & conventions

Settle these once; the phases assume them.

### Wire format: protobuf via Connect-RPC + buf

The primer says "REST API" and "interchange format = protobuf." Those pull in
slightly different directions, so decide this *before* writing endpoints.

**Decision: [Connect-RPC](https://connectrpc.com/) with [buf](https://buf.build/).**

**Why / what's actually happening:**
- You define the API once in `.proto` files (messages + service methods).
- `buf generate` produces **Go server stubs** and **TypeScript client code** from
  the same schema. One source of truth; the frontend can't drift from the backend
  contract, and the new-to-frontend you doesn't hand-write fetch calls.
- Connect speaks gRPC, gRPC-Web, and its own Connect protocol over plain HTTP/1.1,
  and can serve **JSON or binary protobuf on the same endpoints** — curl-able JSON
  in dev, compact binary in prod, no separate REST layer.
- Browsers can't speak raw gRPC; Connect's client handles that. That's the
  specific reason *not* to reach for vanilla gRPC.

**Protobuf is the interchange format for:** all API request/response bodies; SSE
event payloads (define an event envelope message; JSON-encoded protobuf is fine
over SSE since SSE is text — you still get the shared schema + generated types).

**Stays plain JSON:** Firebase JWTs (Firebase owns that format); third-party
webhooks (Resend/SendGrid send what they send).

**Fallback:** if Connect proves too heavy, hand-write Go handlers but **keep
`.proto` as the schema-of-record and codegen the Go structs + TS types.** The
shared schema is the part that protects you as a frontend novice.

### Topology: public website vs. data API are separate surfaces

You don't want the data-accessing API to be a publicly-browsable surface. Note
the honest constraint: a PWA used on phones means the API host is reachable over
the internet — true network isolation would break the app. So "not public" means
**auth on every data endpoint + zero public/marketing content co-mingled with the
API**, achieved by *physically* separating the two surfaces:

```
Firebase Hosting   →  qlab.app (or *.web.app)   static React PWA — public
Cloud Run          →  api.qlab.app              Connect API — JWT required on every call
```

**Why this split (and not one embedded container):**
- The API host carries no HTML/marketing surface at all — it only answers
  authenticated Connect calls. That's the separation you asked for, enforced
  physically rather than by middleware discipline.
- Firebase Hosting is free, gives HTTPS + CDN + atomic deploy + one-command
  rollback, and you're already using Firebase for auth — no new vendor.
- It's actually *simpler* for a frontend novice than embedding React in the Go
  binary: `firebase deploy` ships static files; no `embed.FS`, no hand-written SPA
  fallback, no full backend rebuild for a CSS tweak.

**The one tradeoff:** frontend and API are cross-origin, so the API needs **CORS**
configured (also applies to the SSE `EventSource` stream). One-time, well-trodden
config in Connect. (This supersedes the earlier "single embedded container" idea;
see `docs/decisions/`.)

### Frameworks & libraries

Minimal, boring, and free. **Confirmed** = decided; **Pending** = needs
investigation before adoption.

**Go (backend):**
| Concern | Choice | Status | Why |
|---------|--------|--------|-----|
| Logging | **`slog`** (stdlib), JSON handler in cloud, text locally | Confirmed | Structured, zero-dep, Claude-readable (see Observability). |
| Tracing | **OpenTelemetry-Go**, light spans, exported to **Google Cloud Trace** (free tier) in staging/prod, stdout locally | Confirmed | "Free and easy" tracing; Cloud Trace is free and already in GCP. |
| Testing | **`testify`** (`require`/`assert`/`suite`) + stdlib table tests | Confirmed | Your preference; table tests fit the engine's test matrix. |
| HTTP/middleware | stdlib `net/http` + **`chi`** for middleware (auth, request-id, recovery, CORS) | Confirmed | Connect handlers mount on `net/http`; chi gives clean middleware. |
| DB driver | **`pgx`** (pgxpool) | Confirmed | The modern Postgres driver; works with Neon. |
| Query builder | **`squirrel`** (Masterminds/squirrel) | Confirmed | Fluent builder for dynamic SQL; emits `pgx`-ready query + args. |
| Type-safe query gen | **`sqlc`** (generate Go from SQL) | Confirmed | Compile-time-checked static queries; coexists with squirrel (sqlc for static, squirrel for dynamic). |
| Migrations | **`goose`** | Confirmed | Simple, Go-native, embeddable; runnable locally by Claude. |
| Config | **`kelseyhightower/envconfig`** | Confirmed | Struct-tag env loading; 12-factor; Cloud Run injects env. |
| Lint | **`golangci-lint`** | Confirmed | Standard gate in CI. |
| Proto validation | **`protovalidate`** (buf) | Confirmed | Declarative validation rules in `.proto`. |

**TypeScript (frontend):**
| Concern | Choice | Status | Why |
|---------|--------|--------|-----|
| Build/dev server | **Vite + React + TypeScript** | Confirmed | Standard; fast HMR. |
| Unit/component tests | **Vitest + React Testing Library** | Confirmed | Vitest shares Vite config; RTL is the React standard. |
| E2E | **Playwright** | Confirmed | Drives the real login + queue flows; one tool, all browsers. |
| API client + caching | **Connect-Query** (TanStack Query + the generated Connect client) | Confirmed | Typed calls + caching/refetch/live-ish state with little code. |
| Styling | **Tailwind CSS** | Confirmed | Utility classes = far less CSS for a novice; maps the branding palette to a theme; Claude generates it well. |
| Lint/format | **Biome** | Confirmed | One fast tool instead of ESLint+Prettier. |
| PWA | **`vite-plugin-pwa`** | Confirmed | Generates the service worker + manifest. |
| Auth SDK | **Firebase JS SDK** | Confirmed | Google sign-in. |

**Tooling (shared):** **Yaak** (API client — see below), `buf`, `gcloud`,
`docker` + compose, **mage** (Go-based task runner), optional `lefthook` for
pre-commit hooks.

### Yaak as the API client of record

Use **[Yaak](https://yaak.app/)** as the dev API client. The rule: **every time we
introduce a new behavior variation we want to reproduce, add a Yaak request for
it.** That makes Yaak a living, runnable catalogue of the system's behaviors —
each reschedule scenario from `docs/ALGORITHM.md`'s test matrix, each auth
state (no token / valid / wrong-lab / member-vs-head), each error case.
- Commit the **exported Yaak workspace** to the repo (e.g. `yaak/qlab.yaak.json`)
  so the request collection is versioned and shared.
- Use Yaak environments for `local` / `staging` (never store prod data-access
  creds in the committed workspace).
- These requests double as living documentation and as a manual-regression
  checklist before pushing.

### Notifications: robust, modular, dead-lettered

Built to swap channels later (email now; SMS/push later) and to never lose a
message silently.
- **`Notifier` interface** with a `Channel` abstraction (`EmailChannel` v1;
  `SMSChannel`, `PushChannel` later are additive). Business code calls
  `notify(event)`, never a provider SDK directly.
- **Transactional outbox:** notifications are written to an `outbox` table inside
  the same DB transaction as the event that triggered them, then delivered
  asynchronously. A failed send **never fails the API request** that triggered it.
- **Retry + dead-letter:** a worker drains the outbox with bounded retries
  (exponential backoff); messages that exhaust retries move to a **dead-letter**
  state (`outbox.status = 'dead'`) and **you get alerted** (an alert email to the
  admin address, plus an error log/span you can surface to Claude).
- **Idempotency:** each outbox row has a dedup key so retries don't double-send.

This is detailed in Phase 11 but the **interface + outbox table** should be
designed early (the table lands in Phase 4's schema) so it's not bolted on.

### Observability & debugging-for-Claude

Because Claude is your main debugging engine and staging will produce more data
than you can eyeball, optimize for **selectively feedable** evidence:
- **Structured logs (`slog` JSON)** with a **request id** and **trace id** on
  every line, so any request's full story can be filtered out and handed to Claude
  as a self-contained slice.
- **Spans** (OTel) on every meaningful unit of work (handler, engine call, DB tx,
  notification send), annotated with the relevant ids (`lab_id`, `pool_id`,
  `slot_id`, event type, which `win_start`s ratcheted) — exactly the fields you'd
  want when reconstructing a reschedule.
- **A `lab_id`-scoped state-export endpoint (staging only, head-auth):** dumps a
  lab's full queue + recent events as JSON/proto so you can hand Claude a precise,
  bounded snapshot instead of a wall of logs. (Disabled/locked in prod — see
  test-data asymmetry below.)
- **Generous staging log retention** within free tier; document the exact
  `gcloud logging read` / filter incantations in the runbook so Claude can tell
  you what to run and you paste back the slice.

### Test identities & emails

To exercise multi-tenant + role behavior without juggling many inboxes, use
**Gmail plus-addressing** so a handful of real inboxes back many logical users.
- Maintain **2 real inboxes**: one "admins" inbox and one "members" inbox (a 3rd/4th
  is fine if you want staging fully separate from local).
- Derive per-lab/per-role identities by plus-tagging, and put an **identifying
  detail in the email subject** so a single inbox stays sortable:
  - `youradmin+lab1@gmail.com`, `youradmin+lab2@gmail.com` → all land in the admins
    inbox; subjects like `[lab1][HEAD] …`.
  - `yourmember+lab1@gmail.com`, `yourmember+lab2@gmail.com` → members inbox;
    subjects like `[lab1][MEMBER] …`.
- **Where you'll need to act:** *reserve* these in Phase 0; *wire the invite flow*
  to them in Phase 8; *verify real delivery* in Phase 11. These are the only email
  accounts you must maintain.

### Test-data access: open in staging, guarded in prod

Make demo/test data **trivial for devs to reach in staging** and **difficult in prod**:
- **Staging:** seed scripts create demo labs/users/equipment/queues; a
  **dev-login / impersonation path** lets you (and Claude-prepared requests) act as
  any seeded demo user without the full Google OAuth dance; the state-export
  endpoint (above) is enabled. All of this is gated behind an env flag
  (`QLAB_ENV=staging`) and **compiled/guarded off when `QLAB_ENV=production`.**
- **Prod:** no seed data, no dev-login, no impersonation, no state-export. The only
  way in is real Google sign-in + a real invite. The guard is enforced at startup
  (refuse to enable dev-only routes if `prod`) and asserted in a test.

---

## Phase 0 — Foundations, tooling & the algorithm spec

**Goal:** Every tool installed, accounts created, repo exists, **and the scheduling
engine is specified on paper** — nothing built yet.

**Work:**
- **Finalize `docs/ALGORITHM.md`** (already drafted): review the six flagged decisions in
  §12 and sign off (notably window forward-only and the bench-pool data model). This
  is the "solve it up front" step — the engine is the riskiest logic, so it's pinned
  down before any code.
- Confirm local toolchain: Go, Node.js (current LTS), Docker Desktop, `gcloud`,
  `buf`, Yaak. Add `air` (or similar) for Go live-reload (optional).
- Create cloud accounts/projects (lead time + free-tier signup):
  - **GCP project**, billing enabled; enable Cloud Run, Artifact Registry, Secret
    Manager, Cloud Build, **Cloud Trace** APIs.
  - **Neon** account.
  - **Two Firebase projects** (`qlab-staging`, `qlab-production`); enable **Firebase
    Hosting** on each (for the static PWA) plus Auth.
  - **Resend or SendGrid** account (reserve; decide in Phase 11).
- **Reserve the test inboxes** and the plus-addressing scheme (see conventions).
- Create the GitHub monorepo; branch protection on `main`, squash-merge, auto-delete
  branches.
- Seed the docs skeleton: `README.md`, `docs/ARCHITECTURE.md`, `docs/runbook.md`,
  `docs/decisions/`, root + per-area `CLAUDE.md`.

**Exit criteria:** `gcloud`, `buf`, `docker`, `go`, `node` run locally; Yaak
installed; repo cloned with the docs skeleton; **`docs/ALGORITHM.md` decisions signed
off.**

**Notes:**
- **Firebase vs GCP:** a Firebase "project" *is* a GCP project with Firebase
  features turned on. Two consoles, one underlying thing.
- Treat staging and prod as fully separate from day one; the marginal cost is ~zero
  on free tiers and mixing them later is painful.

---

## Phase 1 — Monorepo skeleton + hello-world Go service

**Goal:** A Go HTTP service that returns 200, with the monorepo layout in place and
structured logging from the very first line.

**Work:**
- Lay out the repo (`backend/`, `frontend/`, `proto/`, `docker-compose.yml`,
  `.github/workflows/`, `docs/`, root `README.md`).
- `backend/cmd/server/main.go`: minimal HTTP server with a `/healthz` endpoint.
- `backend/internal/` for everything not meant to be imported externally.
- **Wire `slog` from the start** (JSON handler, request-id middleware) — don't add
  logging later. A request id on every line is the foundation of the observability
  story.
- Multi-stage `backend/Dockerfile` (build Go binary in a builder stage; copy into a
  distroless/alpine runtime stage). **No frontend embedding** — the PWA ships
  separately via Firebase Hosting (see topology decision).

**Exit criteria:** `docker build` produces an image; running it, `curl
localhost:PORT/healthz` returns 200 and emits a structured log line with a request
id. A Yaak request for `/healthz` exists.

**Notes:**
- Keep `main.go` thin: parse config from env, wire dependencies, start server. All
  logic lives in `internal/`.
- Read config from env now (PORT, `QLAB_ENV`, …) — Cloud Run injects `PORT` and
  you must respect it.

---

## Phase 2 — Local dev with Docker Compose (Claude-operable)

**Goal:** One command brings up the Go service + local Postgres, and **Claude can
drive the whole local environment unattended.**

**Work:**
- `docker-compose.yml`: the Go service (live-reload mount or rebuild) + a
  `postgres` container with a named volume.
- Wire the DB connection string via env; health check that pings the DB on boot.
- `.env.example` documenting required vars; real `.env` gitignored.
- **mage targets Claude needs** (Go-based task runner — `magefile.go`): `up`,
  `down`, `reset` (wipe + recreate), `migrate`, `seed`, `test`, `logs`, `proto`.
  These are the contract for "Claude operates local infra."
- Write `docs/runbook.md` covering exactly these commands.

**Exit criteria:** `mage up` brings up both; the Go service connects to Postgres on
boot; **Claude can run `up`/`reset`/`logs`/`test` end-to-end without you.** Runbook
documents it.

**Notes:**
- **Why local Postgres, not Neon, for dev:** faster, offline, free, wipeable. Match
  the Postgres *version* to Neon's to avoid surprises.
- This is your inner dev loop for the rest of the project — invest in making it fast
  and reliable now.

---

## Phase 3 — CI/CD: API to Cloud Run, PWA to Firebase Hosting

**Goal:** Merge → backend image built & deployed to Cloud Run; frontend built &
deployed to Firebase Hosting — for *both* environments. ("Deploy hello world to
both envs through GitHub Actions," adapted to the two-surface topology.)

**Work:**
- GitHub Actions:
  1. Build the backend image → push to **Google Artifact Registry** → deploy to
     **Cloud Run** (`api-staging` on merge; `api-prod` via a manual gate).
  2. Build the frontend → deploy to **Firebase Hosting** (`staging` channel/site
     on merge; `prod` site via the same manual gate).
- **Workload Identity Federation** so GitHub Actions authenticates to GCP without a
  long-lived service-account key.
- **Promotion strategy:** every merge auto-deploys *staging* (both surfaces); *prod*
  is a manual approval (GitHub Environments with required reviewers) or a tag — and
  per the Guiding constraint, **you** trigger prod. Document it.
- Configure **CORS** on the API for the Hosting origin (the topology tradeoff).

**Exit criteria:** A merge yields a live public Hosting URL serving the hello-world
PWA *and* a live Cloud Run API URL returning hello-world, for **both** staging and
prod; the PWA can reach the API cross-origin (CORS works).

**Notes (infra — read carefully, new territory):**
- **Artifact Registry** = GCP's private Docker registry; Cloud Run pulls from it.
- **Cloud Run** = "give me a container that listens on `$PORT`; I'll run it, scale
  to zero when idle, scale up on traffic." Scale-to-zero ⇒ a cold-start latency on
  the first request after idle — fine for 15 people.
- **Firebase Hosting** = a CDN for your static PWA with free HTTPS and atomic
  deploys; `firebase deploy` ships the `dist/` folder. SPA rewrite (everything →
  `index.html`) is one line of Hosting config — replaces the embedded-Go SPA
  fallback you'd otherwise hand-write.
- **Workload Identity Federation** lets GitHub's OIDC token be trusted by GCP
  directly — no JSON key to leak. More setup, worth doing right once.

---

## Phase 4 — Database: Neon + schema + migrations + seed

**Goal:** The real data model exists, versioned via migrations, on Neon's staging
and prod branches, with **seedable demo data for staging/local.**

**Work:**
- Create the Neon project; create `staging` and `prod` **branches** (cheap
  copy-on-write Postgres instances).
- Migrations (via `goose`) for: `labs`, `users`, `lab_memberships` (with role),
  **equipment as a *pool* of interchangeable benches** (see the data-model note),
  `slots` (id, user_id, lab_id, **pool_id**, **assigned_bench_id** (nullable),
  **priority**, **win_start**, window, duration, **actual_start**, status, note —
  per `docs/ALGORITHM.md` §1.1), **and `outbox`** (for notifications — designed now per
  the notifications convention).
- Run migrations against local Postgres and both Neon branches (locally + via
  `mage migrate`; staging/prod runs are triggered by **you**).
- **Seed scripts** (`mage seed`) that build demo labs/users/bench-pools/queues —
  the same scenarios as `docs/ALGORITHM.md`'s test matrix (single- and multi-bench, gap-
  fill, no-show), so the UI and API have realistic situations to show. **Seeding is
  staging/local only** (guarded by `QLAB_ENV`).

**Exit criteria:** Schema applied to local + both Neon branches; a Go DB layer
(`internal/db`) connects and runs a trivial query against Neon; `mage seed`
populates a demo lab locally; the same seed is reproducible on the Neon staging
branch.

**Notes:**
- **Modeling time:** use **`timestamptz`** for `win_start` / `actual_start`
  (timezones, DST, human-readability); the engine works in absolute instants +
  minute deltas, consistent with `docs/ALGORITHM.md`. `duration` and `window` are minute
  integers.
- **`window` is start-time flexibility, *not* tied to duration** (per `docs/ALGORITHM.md`
  §2). Enforce only `window ≥ 0` at the DB level (`CHECK`). **Do not** add a
  `window ≤ duration` constraint — durations are inviolable and unrelated to the
  window. `window = 0` means *rigid* (`docs/ALGORITHM.md` §2.4).
- **Equipment pool (⚠️ data-model decision, `docs/ALGORITHM.md` §10/§12):** the engine
  fans a single queue out across interchangeable benches and assigns the *specific*
  bench near clock-in — so model `equipment` as a **pool** of benches and keep
  `slots.assigned_bench_id` **nullable** until clocked in (not a hard `equipment_id`
  fixed at booking). Confirm this before writing the migration; it's the one place
  the corrected algorithm reshapes the schema.
- **Statuses:** the `status` enum includes **`NO_SHOW`** (auto-applied when the
  clock-in grace lapses — `docs/ALGORITHM.md` §1.2/§2.3) alongside SCHEDULED / ACTIVE /
  COMPLETE / CANCELLED.
- **Multi-tenancy:** `lab_id` on every tenant-scoped row. Index `lab_id`, and
  `(pool_id, priority)` for queue order plus `(assigned_bench_id, actual_start)` for
  per-bench timeline lookups.
- Neon scale-to-zero idles after inactivity — the weekly cron (Phase 11) doubles as
  a keep-alive.

---

## Phase 5 — Contract layer (protobuf + codegen)

**Goal:** `.proto` schemas defined; Go and TypeScript types generated; an empty
Connect service compiles and serves.

**Work:**
- Create `proto/qlab/v1/*.proto`. Messages for Lab, User, BenchPool + Bench,
  Slot, Membership; a `Slot.Status` enum (incl. `NO_SHOW`); the SSE **event
  envelope** message; a **reschedule result** message (the recomputed schedule —
  per-slot `actual_start`, `assigned_bench`, ratcheted `win_start`, and a
  re-committed/notify flag per `docs/ALGORITHM.md` §5). There is **no** overflow/
  infeasible result — the schedule never fails (`docs/ALGORITHM.md` §8).
- Define the service methods (list slots, create slot, clock in/out, cancel,
  overrun) — stub them; implement in Phase 7.
- `buf.yaml` + `buf.gen.yaml`; wire `buf lint`, `buf breaking`, `buf generate` into
  the mage targets + CI. Add **`protovalidate`** rules (e.g. `window ≥ 0`).
- Generate Go stubs into `backend/internal/gen`, TS into `frontend/src/gen`.
- Mount the generated Connect handlers on the existing server; return
  `Unimplemented` for now.

**Exit criteria:** `mage proto` (`buf generate`) is reproducible; the Go service
serves the Connect endpoints (unimplemented); generated TS types exist; a Yaak
request hits one Connect endpoint and gets `Unimplemented`.

**Notes:**
- **`buf breaking`** in CI catches accidental backwards-incompatible changes — your
  safety net once the frontend depends on these types.
- Version the package (`v1`) from the start.
- Commit generated code OR generate in CI — pick one. Committing is simpler for a
  small team and decouples the frontend build from `buf`.

---

## Phase 6 — Core domain: queue + scheduling engine

**Goal:** Implement the engine **exactly as specified in `docs/ALGORITHM.md`**, as pure,
exhaustively-tested Go.

**Work:**
- Implement `internal/scheduling` as **pure functions** over slot slices — *no DB,
  no HTTP, no clock reads* (the engine contract, `docs/ALGORITHM.md` §10). The corrected
  model is **a single `reschedule(slots, benches, now)`** (a greedy, priority-
  ordered, multi-bench list scheduler with gap-fill — `docs/ALGORITHM.md` §5); cascade
  and pull-forward are the *same* call. No compression, no `MIN_DURATION`, no
  overflow/infeasible result.
- **Table-driven `testify` suite** covering the **full §11 test matrix** (16 cases:
  single-bench, multi-bench, lifecycle) plus invariant assertions (per-bench
  no-overlap, priority respected, durations unchanged, `actual_start ≥ win_start`
  and `win_start` monotonic, ACTIVE/history untouched, determinism).
- Add a **Yaak request per behavior variation** once the API wraps this (Phase 7),
  mirroring the matrix.

**Exit criteria:** Every §11 case passes; the engine has zero dependencies on
DB/HTTP/proto (conversions happen at the edges); `mage test` runs it fast.

**Notes:**
- This is the heart of the product and the easiest thing to get subtly wrong —
  which is exactly why it was specified up front and is built in isolation before
  wiring. Resist writing it inside an HTTP handler.
- The §12 decisions in `docs/ALGORITHM.md` must be signed off (Phase 0) before this
  starts — the implementation encodes them (notably: window forward-only, and the
  bench-pool model).

---

## Phase 7 — API endpoints (wire engine ⇄ DB ⇄ Connect)

**Goal:** Real, persisted operations exposed over the Connect/proto API.

**Work:**
- Implement the Phase 5 stubs as the engine **events** (`docs/ALGORITHM.md` §6): book/
  create, clock-in (→ `ACTIVE`, pin to a bench), clock-out / early-finish (→
  `COMPLETE`), cancel (→ `CANCELLED`), overrun (active past scheduled end), and the
  **no-show sweep** (grace lapsed → `NO_SHOW`). Every event mutates state then calls
  the one `reschedule`.
- Each mutating call: load the pool's open slots **`FOR UPDATE`** → run the pure
  engine (Phase 6) → persist the recomputed schedule **in one transaction** → write
  any resulting notifications to the **outbox** (same tx) → return the updated
  schedule.
- Enforce **`lab_id` scoping in every query** (defense in depth; pairs with Phase 8).
- **Spans** on handler → engine → tx → outbox, annotated with `lab_id`, `pool_id`,
  event type, and per-slot ratchet info (which `win_start`s shifted) (observability
  convention).
- **Add a Yaak request for each behavior variation** (every matrix scenario, each
  error path) — this is where the Yaak catalogue becomes the live regression set.

**Exit criteria:** You can drive a full lifecycle via **Yaak** (or `buf curl`)
against local Postgres: create slots, clock in, overrun → see people behind pushed
(some hopping benches); finish early / cancel → see pull-forward; let a clock-in
grace lapse → see `NO_SHOW` re-flow the queue. Each is a saved Yaak request.

**Notes:**
- Keep proto/DB ↔ domain conversions at the handler boundary so the engine stays
  pure.
- The **no-show sweep** needs a trigger (the weekly cron is too coarse) — a
  lightweight periodic check (e.g. a short-interval Cloud Scheduler ping, or
  evaluating grace lazily on the next event/read). Pick one; note it.
- Concurrency: the per-operation transaction + `SELECT … FOR UPDATE` on the pool's
  slots is what stops two simultaneous clock-outs from corrupting the schedule
  (`docs/ALGORITHM.md` §10).

---

## Phase 8 — Auth: Firebase + JWT validation; staging-open / prod-locked

**Goal:** Every API call is authenticated as a real user scoped to their lab — with
an easy demo-login path in staging and none in prod.

**Work:**
- In each Firebase project, enable **Google as a sign-in provider** (Login with
  Google only, per the primer).
- Backend middleware: extract `Authorization: Bearer <jwt>`, verify against
  Firebase's public keys (**Firebase Admin SDK for Go** — handles key rotation),
  resolve Firebase UID → your `users` row.
- Map user → membership(s) → inject `lab_id` + role into request context; reject if
  no membership. Head-only authorization check for admin actions (inviting members).
- **Invite-only membership:** lab head adds a member by email; first login with a
  matching invited email provisions the `users` row + membership. Wire the invite
  flow to the **plus-addressed test inboxes**.
- **Staging-only dev-login / impersonation** path (act as any seeded demo user
  without the OAuth dance), and the **`lab_id`-scoped state-export endpoint** —
  both **guarded by `QLAB_ENV`** and **refused at startup when `production`**. Add a
  test asserting they're off in prod.

**Exit criteria:** Unauthenticated calls rejected; a valid Firebase token lets you
hit the API and see only your lab's data; head-only endpoints reject members; in
staging you can impersonate a demo user in one step; in a prod-config build the
dev-login/export routes are absent (asserted by test). Yaak requests cover
no-token / valid / wrong-lab / member-vs-head.

**Notes (auth — you know JWTs; here's the setup side):**
- **What Firebase Auth gives you:** it runs the Google OAuth dance in the browser
  and hands the frontend a signed JWT. Your backend never sees Google credentials —
  it only *verifies* Firebase's JWT. That's why this phase is mostly "validate a
  token," which you're comfortable with.
- **Verification ≠ decoding:** verify signature against Firebase's rotating public
  keys, check `aud`/`iss` match your project, check expiry. The Admin SDK does all
  of this; don't hand-roll it.
- **Two Firebase projects** = two `aud`/issuer values; staging and prod backends
  each verify against their own project. Drive by config.
- **UID vs your user row:** store `firebase_uid` on the user row; map Firebase's
  stable external identity to your internal `users.id` explicitly.
- **The dev-login path is the single most dangerous thing in the codebase** if it
  ever ships to prod — hence the startup refusal *and* the test. Treat that guard
  as load-bearing.

---

## Phase 9 — Frontend scaffold: React PWA + Firebase login end-to-end

**Goal:** A React app that logs in with Google, gets a token, and makes one
authenticated API call. Your first real frontend work.

**Work:**
- Scaffold **Vite + React + TypeScript**. Add **Tailwind**, **Biome**, **Vitest +
  RTL**, **Playwright**.
- Add the Firebase JS SDK; implement Google sign-in (popup/redirect) → obtain the
  JWT.
- Wire the **generated Connect TS client** (via **Connect-Query**) to attach the
  JWT and call one real endpoint (list slots). Confirm the data round-trips
  cross-origin (CORS).
- PWA basics: `manifest.json` + service worker via **`vite-plugin-pwa`**.
- Deploy to the **staging Firebase Hosting** site (Phase 3 pipeline).

**Exit criteria:** In the browser, "Login with Google" works against the staging
Firebase project, and the app displays real data from the staging API. A Playwright
test drives login → list.

**Notes (frontend — orientation you need):**
- **Mental model:** the frontend is a static bundle of HTML/JS/CSS that runs in the
  user's browser and talks to your Go API over HTTP. There's no frontend "server"
  running your code — the static files are served by Firebase Hosting (CDN); the
  Go API is a separate origin.
- **Vite dev server** runs locally on its own port and proxies API calls to your
  local Go service; in prod the built static files live on Hosting.
- **You already have typed API access** from Phase 5 codegen — you call generated
  functions, not raw `fetch`. Deliberate, to shield you from contract drift.
- **PWA = an installable website** (home-screen icon, offline-ish, app-like). The
  manifest describes the installable app; the service worker caches/works offline.
  Let the plugin generate the hard parts.
- Don't over-invest in styling yet; get the data flow correct first.

---

## Phase 10 — Core UI

**Goal:** The actual product UI: queue view, timeline view, clock in/out, live
updates.

**Work:**
- **Queue view:** the single priority-ordered line with status, user, time, window.
- **Timeline view:** slots over time, **fanned out per bench** (`docs/ALGORITHM.md`
  §1.4) — where the amber "flexible" vs rigid distinction and overrun-red from the
  branding matter; surface **pushed / re-committed** slots and `NO_SHOW`s clearly,
  and make execution-order-≠-priority-order legible when gap-fill reorders.
- **Clock in / out / cancel** actions calling Phase 7 endpoints.
- **Live updates via SSE:** backend pushes queue-changed events (proto envelope,
  Phase 5); frontend subscribes via `EventSource` and re-renders (CORS applies to
  the stream). On Cloud Run, SSE works over HTTP/1.1 — configure the request
  timeout for long-lived streams.
- Apply branding: dark `#0F1117`, teal `#00C9A7`, amber `#F59E0B`, red `#F87171`,
  monospace for all time/data elements (map to the Tailwind theme).

**Exit criteria:** Two browsers open the same lab; one clocks out and the other
sees the queue update live without refresh. Pushed / re-committed slots are visible.
Playwright covers the clock-out → live-update path.

**Notes:**
- **Why SSE not WebSockets:** one-directional (server→client), simpler, plain HTTP,
  auto-reconnect — a perfect fit for "push me queue updates" when all writes go
  through normal API calls. Less to get wrong.
- This phase is the bulk of the frontend learning curve. Keep components small; lean
  on the generated types and Connect-Query.

---

## Phase 11 — Notifications + weekly cron summary

**Goal:** The robust, modular, dead-lettered notification system fires on the right
events; weekly summary runs on a schedule.

**Work:**
- Implement the **`Notifier` + `Channel`** abstraction with an **`EmailChannel`**
  over Resend/SendGrid (free tier). API key in **GCP Secret Manager**, injected into
  Cloud Run as a secret reference — never in the repo.
- Implement the **outbox worker:** drain `outbox`, deliver via the channel, bounded
  retries with backoff, idempotent sends; exhausted messages → **dead-letter** +
  **alert the admin address** (and an error span/log). Triggers: bench freed (notify
  whoever the reschedule now places next), **slot re-committed** (a `win_start`
  ratcheted past its window — notify of the new start, per `docs/ALGORITHM.md` §2.2),
  slot pulled forward, and `NO_SHOW` recorded.
- **Weekly cron:** Cloud Scheduler → a backend endpoint that compiles the lab usage
  summary and emails the head; also keeps Neon warm. Protect it (OIDC from Cloud
  Scheduler, or a shared secret) so the public can't trigger it.
- Verify real delivery against the **plus-addressed test inboxes**, checking the
  subject-line identifiers route correctly.

**Exit criteria:** A real test email arrives for each trigger at the right inbox
with the right subject tag; a forced send-failure lands in dead-letter and produces
an admin alert; the cron endpoint, when hit, sends a summary; the schedule is
configured. Yaak requests exercise each trigger and the dead-letter path.

**Notes:**
- The outbox makes delivery **decoupled and durable** — the triggering API request
  succeeds regardless of email provider health, and nothing is lost silently.
- **Secret Manager** is the right home for the email API key (and any secret); Cloud
  Run mounts a secret as an env var directly — set this in the deploy step
  (Phase 3).
- Swapping in SMS/push later is **adding a `Channel`**, not touching business code —
  that's the point of the abstraction.

---

## Phase 12 — Production hardening, PWA polish, phone testing

**Goal:** Ship-ready: the two-surface deployment is solid, the install flow works on
a phone, and edge cases are handled.

**Work:**
- Confirm the split deploy end-to-end: PWA on Hosting, API on Cloud Run, CORS +
  Firebase auth working from a real device.
- Verify the **PWA install flow on a real phone** ("Add to Home Screen"; requires
  HTTPS — both Hosting and Cloud Run give it for free).
- **Edge-case hardening:** pushed-far / re-committed slots surfaced in the UI;
  `NO_SHOW` re-flow; concurrent clock-outs; empty queues; idle benches with nobody
  available; a member with no lab; expired tokens (frontend refreshes the Firebase
  token); dead-letter alerting verified.
- Final pass on **docs** so a fresh Claude session can bootstrap anywhere:
  `docs/ARCHITECTURE.md` current, runbook complete, per-area `CLAUDE.md` accurate,
  decision log captures the topology + `window` / bench-pool choices.

**Exit criteria:** The full app works from a phone against staging (you), then prod
(you); "Add to Home Screen" works; a slot pushed far / no-showed re-flows gracefully
end-to-end; the dead-letter alert fires on a forced failure; docs let a cold-start
Claude session orient quickly.

**Notes:**
- **Why two surfaces, not one embedded container** (recap): physical separation of
  the public PWA from the data API (your point about not exposing the core service),
  free Hosting with HTTPS/CDN/rollback, and a simpler frontend deploy loop. The
  tradeoff is CORS, configured once.
- **SPA fallback** is a one-line Hosting rewrite (`** → /index.html`) — test deep
  links / refresh on a client route.
- HTTPS is mandatory for service workers / PWA install — both your origins are HTTPS
  by default until you add a custom domain.

---

## Post-MVP (parking lot)

From the primer, not scheduled here but design with them in mind: multiple
equipment types, maintenance/calibration scheduling, training gates, consumables,
experiment logs, onboarding checklists, noticeboard, protocol library, lab-meeting
scheduling. Most are additive tables + endpoints + UI. Engine-relevant ones tracked
in `docs/ALGORITHM.md`: **max-priority slots** (jump to the front of the queue; never
interrupt an active experiment — §8.3) and richer booking placement. Note that
**wall-clock anchors are permanently out of scope** (§8.2: experiments necessarily
run over) and that gap-fill placement is already in the v1 engine. The scheduling
engine and contract layer should not need to change to support the non-scheduling
features.

## Suggested PR slicing

Each phase is roughly one or a few PRs; keep PRs deployable. `docs/ALGORITHM.md` sign-off
(Phase 0) and the pure engine (Phase 6) are their own reviewable units. Recommended
order of *first* deploys to prod: Phase 3 (hello world, both surfaces), then auth
(8) + DB (4) before any real feature, so prod always has working auth + persistence
under whatever ships next.
