# frontend

The React + TypeScript PWA, deployed to Firebase Hosting.

> **Status:** Phase 9 (frontend scaffold). The Vite/React/TypeScript app is in
> place — Google sign-in (Firebase), the generated Connect client wired through
> Connect-Query, and one real authenticated call (`ListSlots`). The product UI
> (queue + timeline, clock in/out, live updates) lands in Phase 10; PWA basics +
> Playwright + the staging-deploy swap land in this phase's second PR.
>
> The Phase 3 hello-world (`public/` + `build.sh`) still backs the CD deploy until
> that swap; `vite build` currently sets `publicDir: false` so it doesn't clash.

## Local development

    cd frontend
    npm install
    npm run dev          # Vite dev server on http://localhost:5173

`npm run dev` needs the local API + Auth emulator running (`mage startStack` from
the repo root). Config comes from `.env.local` (copy `.env.example`); the defaults
target the local stack and the Auth emulator.

Scripts: `dev`, `build`, `test` (Vitest), `typecheck`, `lint`/`lint:fix` (Biome),
`format`.

## Signing in / testing against the API

Two ways to get an authenticated session (both feed the same Connect transport,
which attaches `Authorization: Bearer <token>` and `X-QLab-Lab`):

1. **Google sign-in** — the production path. Locally the Firebase SDK is pointed
   at the Auth emulator, so the popup signs you in as any email with no real
   Google account. The email must be invited (provisioned) — see below.
2. **Dev token panel** — paste an operator-minted ID token plus a lab + pool id
   to act as a seeded user without the OAuth dance (decision 0008). This is the
   **staging-test** path: mint against staging with no new real identities, and
   prod stays untouched. There is no public `ListPools` RPC yet (Phase 10), so the
   lab/pool ids are taken from the operator `ProvisionLab` response.

Provision a workspace and mint a token via the operator surface — see
`docs/runbook.md` → "Operator surface" and "Frontend dev loop".

## Stack

Vite · React · TypeScript · Tailwind (v4, `@tailwindcss/vite`) · Connect-Query
(generated Connect client) · Firebase JS SDK (Google sign-in) · Biome · Vitest +
RTL. (vite-plugin-pwa + Playwright land in this phase's second PR.)

## Conventions

- API access uses the **generated** Connect TS client (`src/protogen`, from
  `proto/`) — no hand-written `fetch` calls.
- The app is a static bundle served from a CDN; it talks to the Cloud Run API
  cross-origin (CORS). There is no frontend server running our code.

See `frontend/CLAUDE.md`, `docs/PLAN.md` (Phases 9, 10) and `docs/ARCHITECTURE.md`.
