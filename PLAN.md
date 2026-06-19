# Quelab — Build Plan

> Lab equipment scheduling PWA with a live queue + cascade-scheduling engine.
> See `prompt.txt` for the full product primer. This file is the *engineering*
> roadmap: broad phases, ordered, with exit criteria.

## How to read this plan

You're a backend Go engineer. The parts you already know (Go, SQL, CI/CD
mechanics, what JWTs are) are kept terse. The parts that are new to you
(frontend tooling, cloud infra wiring, *setting up* auth rather than consuming
it) get a **"Why / what's actually happening"** note so you're not pattern-matching
blind.

Each phase has:
- **Goal** — the one-sentence outcome.
- **Work** — the concrete steps.
- **Exit criteria** — how you know you're done and can move on.
- **Notes** — gotchas, and explanations of unfamiliar tooling.

Don't skip ahead. Each phase deliberately ends in something *deployed and
verifiable*, so that when something breaks later you know which layer to blame.

---

## Cross-cutting decision: the wire format (protobuf)

The primer says "REST API" and "interchange format = protobuf." Those pull in
slightly different directions, so decide this *before* writing endpoints,
because it shapes the whole contract layer.

**Recommendation: use [Connect-RPC](https://connectrpc.com/) with [buf](https://buf.build/).**

**Why / what's actually happening:**
- You define your API once in `.proto` files (messages + service methods).
- `buf generate` produces **Go server stubs** and **TypeScript client code**
  from the same schema. One source of truth; the frontend can't drift from the
  backend contract, and the new-to-frontend you doesn't hand-write fetch calls.
- Connect speaks **three protocols over plain HTTP/1.1**: gRPC, gRPC-Web, and
  its own Connect protocol. Crucially it can serve **JSON or binary protobuf**
  on the *same* endpoints — so you get curl-able JSON in dev and compact binary
  in prod, no separate REST layer.
- It works through Cloud Run's HTTP/1.1 load balancer without the gRPC headaches.
- Browsers can't speak raw gRPC; Connect's client handles that for you. This is
  the specific reason *not* to reach for vanilla gRPC here.

**Where protobuf is the interchange format:**
- All API request/response bodies (slots, labs, equipment, memberships).
- **SSE event payloads** for live updates — define an event envelope message and
  serialize each event (JSON-encoded protobuf is fine over SSE, since SSE is
  text; you still get the shared schema + generated types).

**What stays plain JSON / not-proto:**
- Firebase JWTs (Firebase owns that format).
- Webhook payloads from third parties (Resend/SendGrid send what they send).

If you later decide Connect is too much ceremony, the fallback is: hand-written
REST handlers in Go, but still keep `.proto` as the schema-of-record and codegen
the Go structs + TS types. Don't skip the shared schema either way — that's the
part that protects you as a frontend novice.

---

## Phase 0 — Foundations & tooling

**Goal:** Every tool installed, accounts created, repo exists, nothing built yet.

**Work:**
- Confirm local toolchain: Go, Node.js (use the current LTS), Docker Desktop,
  `gcloud` CLI, `buf` CLI. Add `air` or similar for Go live-reload (optional).
- Create cloud accounts/projects (these have lead time and free-tier limits):
  - **GCP project** with billing enabled; enable Cloud Run, Artifact Registry,
    Secret Manager, Cloud Build APIs.
  - **Neon** account.
  - **Two Firebase projects** (`quelab-staging`, `quelab-prod`) — Firebase is its
    own console layered on top of a GCP project.
  - **Resend or SendGrid** account (decide later; just reserve it).
- Create the GitHub repo/monorepo, enable branch protection on `main`, squash
  merge only, auto-delete branches.

**Exit criteria:** `gcloud`, `buf`, `docker`, `go`, `node` all run locally;
empty repo cloned with a README.

**Notes:**
- **Firebase vs GCP:** a Firebase "project" *is* a GCP project with Firebase
  features turned on. Don't be confused that you log into two different consoles
  for the same underlying thing.
- Treat staging and prod as fully separate from day one. Mixing them later is
  painful; the marginal cost now is near zero on free tiers.

---

## Phase 1 — Monorepo skeleton + hello-world Go service

**Goal:** A Go HTTP service that returns 200, with the monorepo layout in place.

**Work:**
- Lay out the repo per the primer (`backend/`, `frontend/`, `docker-compose.yml`,
  `.github/workflows/`, root `README.md`).
- `backend/cmd/server/main.go`: minimal HTTP server with a `/healthz` endpoint.
- `backend/internal/` for everything not meant to be imported externally.
- Add the multi-stage `backend/Dockerfile` (build Go binary in a builder stage,
  copy into a distroless/alpine runtime stage). Frontend embedding comes later —
  for now just serve the API.

**Exit criteria:** `docker build` produces an image; running it, `curl
localhost:PORT/healthz` returns 200.

**Notes:**
- Keep `main.go` thin: parse config from env, wire dependencies, start server.
  All logic lives in `internal/`. This pays off when you add auth/DB.
- Read config from env vars now (PORT, etc.) — Cloud Run injects `PORT` and you
  must respect it.

---

## Phase 2 — Local dev with Docker Compose

**Goal:** `docker compose up` gives you the Go service + a local Postgres,
talking to each other.

**Work:**
- `docker-compose.yml`: the Go service (with live-reload mount or rebuild) plus a
  `postgres` container with a named volume for persistence.
- Wire the Go service's DB connection string to the compose Postgres via env.
- Add a `.env.example` documenting required vars; keep real `.env` gitignored.

**Exit criteria:** One command brings up both; the Go service connects to local
Postgres on boot (health check that pings the DB).

**Notes:**
- **Why local Postgres instead of Neon for dev:** faster, offline, free, and you
  can wipe it freely. Neon branches are for staging/prod. Keep the Postgres
  *version* matched to Neon's to avoid surprises.
- This is your inner dev loop for the rest of the project — invest in making it
  fast and reliable now.

---

## Phase 3 — CI/CD to Cloud Run (both environments)

**Goal:** Push to `main` (or merge a PR) → image built → deployed to Cloud Run.
This is the "deploy hello world to both envs through GitHub Actions" milestone
from the primer.

**Work:**
- GitHub Actions workflow:
  1. Build the backend image.
  2. Push to **Google Artifact Registry**.
  3. Deploy to Cloud Run (`quelab-staging` on merge; `quelab-prod` via a manual
     gate or a tag/release — decide your promotion strategy).
- Set up **Workload Identity Federation** so GitHub Actions authenticates to GCP
  without a long-lived service-account key.

**Exit criteria:** A merge results in a live, public Cloud Run URL returning your
hello-world response, for *both* staging and prod.

**Notes (infra — read this carefully, it's new territory):**
- **Artifact Registry** = GCP's private Docker registry. Cloud Run pulls images
  from here.
- **Cloud Run** = "give me a container that listens on `$PORT`, I'll run it,
  scale it to zero when idle, and scale up on traffic." You don't manage servers.
  Scale-to-zero means a "cold start" latency on the first request after idle —
  fine for a 15-person lab.
- **Workload Identity Federation** is the modern alternative to downloading a
  service-account JSON key into GitHub secrets. It lets GitHub's OIDC token be
  trusted by GCP directly. More setup, but no secret to leak. Worth doing right
  the first time.
- **Promotion strategy:** simplest safe option — every merge auto-deploys
  staging; prod deploy is a manual "approve" step (GitHub Environments with
  required reviewers) or triggered by a git tag. Pick one and document it.

---

## Phase 4 — Database: Neon + schema + migrations

**Goal:** The real data model exists, versioned via migrations, on Neon's
staging and prod branches.

**Work:**
- Create the Neon project; create `staging` and `prod` **branches** (Neon
  branches are cheap copy-on-write Postgres instances).
- Pick a migration tool (`goose`, `golang-migrate`, or `atlas`). Write migrations
  for: `labs`, `users`, `lab_memberships` (with role), `equipment`, `slots`
  (fields per the primer: id, user_id, equipment_id, lab_id, start, duration,
  window, status, note).
- Run migrations against local Postgres (Phase 2) and both Neon branches.
- Wire migration execution into CI or a documented manual step.

**Exit criteria:** Schema applied to local + both Neon branches; a Go DB layer
(`internal/db` or similar) can connect and run a trivial query against Neon.

**Notes:**
- **Modeling `start`:** the primer offers "mins from epoch or absolute
  timestamp." Decide now and be consistent — `timestamptz` is the safer default
  (timezones, DST, human-readability) unless the cascade math is dramatically
  cleaner in integer minutes. The cascade engine (Phase 6) will lean on this, so
  don't defer it.
- **`window = 0` means fixed.** Enforce non-negative window at the DB level
  (`CHECK`).
- **Multi-tenancy:** `lab_id` on every tenant-scoped row. Add indexes on
  `lab_id` and on `(equipment_id, start)` for queue ordering.
- Neon scale-to-zero idles after inactivity — the weekly cron (Phase 11) doubles
  as a keep-alive.

---

## Phase 5 — Contract layer (protobuf + codegen)

**Goal:** `.proto` schemas defined; Go and TypeScript types generated; an empty
Connect service compiles and serves.

**Work:**
- Create a `proto/` tree (e.g. `proto/quelab/v1/*.proto`). Define messages for
  Lab, User, Equipment, Slot, Membership, and a `Slot.status` enum.
- Define the service methods you'll need (list slots, create slot, clock in/out,
  cancel, etc.) — stub them; implement in Phase 7.
- Add `buf.yaml` + `buf.gen.yaml`; wire `buf lint`, `buf breaking`, and
  `buf generate` into the Makefile/CI.
- Generate Go stubs into `backend/internal/gen` and TS into a
  `frontend/src/gen` (frontend consumes these in Phase 9).
- Mount the generated Connect handlers on your existing HTTP server; return
  `Unimplemented` for now.

**Exit criteria:** `buf generate` is reproducible; the Go service serves the
Connect endpoints (returning unimplemented); generated TS types exist.

**Notes:**
- **`buf breaking`** in CI catches accidental backwards-incompatible schema
  changes — your safety net once the frontend depends on these types.
- Version the package (`v1`) from the start so you have room to evolve.
- Commit generated code OR generate in CI — pick one policy. Committing is
  simpler for a small team and makes the frontend build not depend on `buf`.

---

## Phase 6 — Core domain: queue + cascade engine

**Goal:** The differentiating cascade/pull-forward logic, as pure, well-tested Go.

**Work:**
- Implement in `internal/scheduling` as **pure functions** over slot slices —
  *no DB, no HTTP*. Input: ordered slots + an event (overrun/delay, cancel/early
  finish). Output: the new slot times (or an "unresolvable" flag).
- Cascade (delay): walk from the affected index in start-time order; each slot
  absorbs `min(remainingDelta, slot.window)`; shift its start; reduce delta; stop
  at zero; flag unresolvable if delta remains at queue end. Fixed slots
  (`window == 0`) absorb nothing and get pushed — including pushing other fixed
  slots, per the primer.
- Pull-forward (cancel/early finish): walk forward, pulling slots earlier within
  their windows to fill the gap.
- **Table-driven unit tests** covering: zero-window chains, full absorption,
  partial absorption, unresolvable overflow, pull-forward, and fixed-pushes-fixed.

**Exit criteria:** The engine is fully unit-tested and has zero dependencies on
DB/HTTP/proto types (convert at the edges).

**Notes:**
- This is the heart of the product and the easiest thing to get subtly wrong.
  Build it in isolation, prove it with tests, *then* wire it to persistence and
  the API. Resist the urge to write it inside an HTTP handler.
- Decide transactional semantics: a cascade touches many rows; do it in a single
  DB transaction (Phase 7) so the queue is never observed half-shifted.

---

## Phase 7 — API endpoints (wire engine ⇄ DB ⇄ Connect)

**Goal:** Real, persisted operations exposed over the Connect/proto API.

**Work:**
- Implement the service methods stubbed in Phase 5: CRUD for slots/equipment,
  clock-in (→ `active`), clock-out (→ `complete`, triggers pull-forward), cancel
  (→ `cancelled`, triggers pull-forward), overrun handling (triggers cascade).
- Each mutating call: load the lab's queue → run the pure engine (Phase 6) →
  persist all changes in one transaction → return the updated queue.
- Enforce **`lab_id` scoping in every query** (defense in depth; pair with
  Phase 8 auth).

**Exit criteria:** You can drive a full queue lifecycle via `buf curl` (or
grpcurl/JSON) against local Postgres: create slots, clock in, overrun, see the
cascade reflected; cancel, see pull-forward.

**Notes:**
- Keep the proto/DB ↔ domain conversions at the handler boundary so the engine
  stays pure.
- Concurrency: two people clocking out at once shouldn't corrupt the queue. Rely
  on the per-operation transaction + appropriate row locking
  (`SELECT … FOR UPDATE` on the lab's slots).

---

## Phase 8 — Auth: Firebase + JWT validation middleware

**Goal:** Every API call is authenticated as a real user, scoped to their lab.

**Work:**
- In each Firebase project, enable **Google as a sign-in provider** (Login with
  Google only, per the primer).
- Backend: middleware that extracts the `Authorization: Bearer <jwt>`, verifies
  it against Firebase's public keys (use the Firebase Admin SDK for Go — it
  handles key rotation), and resolves the Firebase UID → your `users` row.
- Map user → lab membership(s) → inject `lab_id` + role into the request context;
  reject if no membership. Add a head-only authorization check for admin actions
  (inviting members).
- Implement invite-only membership: lab head adds a member by email; first login
  with a matching invited email provisions the `users` row + membership.

**Exit criteria:** Unauthenticated calls are rejected; a valid Firebase token
(grab one manually for testing) lets you hit the API and only see your lab's
data; head-only endpoints reject members.

**Notes (auth — you understand JWTs, here's the setup side):**
- **What Firebase Auth gives you:** it runs the Google OAuth dance in the browser
  and hands the frontend a signed JWT. Your backend never sees Google
  credentials — it only *verifies* Firebase's JWT. That's why this phase is
  mostly "validate a token," which you're comfortable with.
- **Verification ≠ just decoding:** verify signature against Firebase's rotating
  public keys, check `aud`/`iss` match your project, check expiry. The Admin SDK
  does all of this; don't hand-roll it.
- **Two Firebase projects** = two different `aud`/issuer values. Your staging and
  prod backends must each verify against their own project. Drive this by config.
- **UID vs your user row:** Firebase UID is the stable external identity; your
  `users.id` is internal. Map between them explicitly (store `firebase_uid` on
  the user row).

---

## Phase 9 — Frontend scaffold: React PWA + Firebase login end-to-end

**Goal:** A React app that logs in with Google, gets a token, and makes one
authenticated API call. This is your first real frontend work.

**Work:**
- Scaffold with **Vite + React + TypeScript** (`npm create vite@latest`). Vite is
  the build tool/dev server — think of it as the frontend's compiler + hot-reload.
- Add the Firebase JS SDK; implement Google sign-in (popup/redirect) → on success
  you get the user's JWT.
- Wire the **generated Connect TS client** (from Phase 5) to attach the JWT and
  call one real endpoint (e.g. list slots). Confirm the data round-trips.
- Add PWA basics: a `manifest.json` and a service worker (use the
  `vite-plugin-pwa` plugin — it generates the service worker for you).

**Exit criteria:** In the browser, "Login with Google" works against the staging
Firebase project, and the app displays real data from your API.

**Notes (frontend — the orientation you need):**
- **Mental model:** the frontend is a static bundle of HTML/JS/CSS that runs in
  the user's browser and talks to your Go API over HTTP. There's no frontend
  "server" running your code (until you embed it in the Go binary for serving the
  files — Phase 12).
- **Vite dev server** runs locally on its own port and proxies API calls to your
  Go service; in production the built static files are served by the Go binary.
- **You already have typed API access** because of Phase 5 codegen — you call
  generated functions, not raw `fetch`. This is deliberately chosen to shield you
  from frontend-contract drift.
- **PWA = a website that can be "installed"** (home-screen icon, offline-ish,
  app-like). The `manifest.json` describes the installable app; the service
  worker is a background script for caching/offline. Let the plugin generate the
  hard parts.
- Don't over-invest in styling yet; get the data flow correct first.

---

## Phase 10 — Core UI

**Goal:** The actual product UI: queue view, timeline view, clock in/out, live
updates.

**Work:**
- **Queue view:** ordered list of slots with status, user, time, window.
- **Timeline view:** visual representation of slots over time (this is where the
  amber "dynamic" vs fixed distinction and overrun-red from the branding matter).
- **Clock in / clock out / cancel** actions calling Phase 7 endpoints.
- **Live updates via SSE:** backend pushes queue-changed events (proto envelope,
  per Phase 5); frontend subscribes and re-renders. Use `EventSource` on the
  client; on Cloud Run, SSE works over HTTP/1.1 (mind request timeout limits —
  Cloud Run allows long-lived requests but configure the timeout).
- Apply the branding: dark `#0F1117`, teal `#00C9A7`, amber `#F59E0B`, red
  `#F87171`, monospace for all time/data elements.

**Exit criteria:** Two browsers open the same lab; one clocks out and the other
sees the queue update live without refresh.

**Notes:**
- **Why SSE not WebSockets:** SSE is one-directional (server→client), simpler,
  works over plain HTTP, and auto-reconnects — a perfect fit for "push me queue
  updates" when all writes go through normal API calls. Less to get wrong than
  WebSockets.
- This phase is the bulk of the frontend learning curve. Expect to spend time on
  React state/rendering. Keep components small; lean on the generated types.

---

## Phase 11 — Notifications + weekly cron summary

**Goal:** Emails fire on the right events; weekly summary runs on a schedule.

**Work:**
- Integrate Resend or SendGrid (free tier). Store the API key in **GCP Secret
  Manager**, injected into Cloud Run as an env var/secret reference — never in
  the repo.
- Trigger emails on: equipment released (notify next in queue), cascade delay
  applied (notify affected users of new ETA), slot cancelled (notify affected
  users after pull-forward).
- **Weekly cron:** Cloud Scheduler → hits a backend endpoint that compiles the
  lab usage summary and emails the lab head. This also keeps Neon warm.

**Exit criteria:** A real test email arrives for each trigger; the cron endpoint,
when hit, sends a summary; the schedule is configured.

**Notes:**
- Send emails **asynchronously / best-effort** — a failed email must not fail the
  API request that triggered it. Fire-and-forget with logging, or a lightweight
  queue.
- **Secret Manager** is the right home for the email API key (and any other
  secret). Cloud Run can mount a secret as an env var directly — set this in the
  deploy step (Phase 3).
- Protect the cron endpoint (OIDC from Cloud Scheduler, or a shared secret) so it
  can't be triggered by the public.

---

## Phase 12 — Single-container serving, PWA hardening, phone testing

**Goal:** Ship-ready: one container serves both API and frontend; install flow
works on a phone; edge cases handled.

**Work:**
- Update the multi-stage Dockerfile: build the React app (Vite `build`), embed
  the static files into the Go binary (`embed.FS`), and serve them from the Go
  service (API under `/api` or the Connect paths, static files everywhere else
  with SPA fallback to `index.html`).
- Verify the PWA install flow on a real phone (requires HTTPS — Cloud Run gives
  you that for free).
- Edge-case hardening: unresolvable cascades (surface the flag in the UI),
  concurrent clock-outs, empty queues, a member with no lab, expired tokens
  (frontend refreshes the Firebase token).

**Exit criteria:** A single deployed Cloud Run container serves the full app;
"Add to Home Screen" works on a phone; the cascade engine's unresolvable case is
handled gracefully end-to-end.

**Notes:**
- **Why embed in the Go binary:** one artifact, one deploy, one origin (no CORS
  between frontend and API), and it matches the primer's stated lean. The
  tradeoff is that a frontend-only change requires a full rebuild/redeploy —
  acceptable at this scale.
- **SPA fallback:** unknown non-API routes must return `index.html` so client-side
  routing works on refresh/deep-link. Easy to forget; test it.
- HTTPS is mandatory for service workers / PWA install — Cloud Run's default
  `*.run.app` domain is HTTPS, so you're covered until you add a custom domain.

---

## Post-MVP (parking lot)

From the primer, not scheduled here but design with them in mind:
multiple equipment types, maintenance/calibration scheduling, training gates,
consumables, experiment logs, onboarding checklists, noticeboard, protocol
library, lab meeting scheduling. Most are additive tables + endpoints + UI;
the cascade engine and contract layer should not need to change to support them.

## Suggested PR slicing

Each phase is roughly one or a few PRs. Keep PRs deployable. Recommended order of
*first* deploys to prod: Phase 3 (hello world), then auth (8) + DB (4) before any
real feature, so prod always has working auth + persistence under whatever you
ship next.
