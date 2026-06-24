# frontend

The React + TypeScript PWA, deployed to Firebase Hosting.

> **Status:** Phase 9 (frontend scaffold). The Vite/React/TypeScript app is in
> place — Google sign-in (Firebase), the generated Connect client wired through
> Connect-Query, and one real authenticated call (`ListSlots`). The CD pipeline now
> ships the real `vite build` to Firebase Hosting (the Phase 3 hello-world is gone);
> the API URL + Firebase web config are injected at build time (see
> `docs/deploy.md`). The product UI (queue + timeline, clock in/out, live updates)
> lands in Phase 10; PWA basics, Playwright, and the in-app staging dev switcher
> are follow-ups.

## Local development

    cd frontend
    npm install
    npm run dev          # Vite dev server on http://localhost:5173

`npm run dev` needs the local API + Auth emulator running (`mage startStack` from
the repo root). Config comes from `.env.local` (copy `.env.example`); the defaults
target the local stack and the Auth emulator. Locally the app calls the API
**same-origin** through the Vite proxy (`vite.config.ts` → `:8090`), so there is no
cross-origin/CORS step on localhost; staging/prod set `VITE_API_BASE_URL` to the
real API URL and are genuinely cross-origin.

Scripts: `dev`, `build`, `test` (Vitest), `typecheck`, `lint`/`lint:fix` (Biome),
`format`.

## The dev switcher

Sign in **once** as the operator (Google), provision or load a demo workspace, then
act as any user in it — fluidly, without pasting tokens. The operator's verified login
drives the staging/local-only operator surface (`qlab.dev.v1`) against an email
allowlist (`OPERATOR_ALLOWED_EMAILS`); switching users mints and caches an ID token
per user, so switching back is instant. Two identities, two transports — see
`frontend/ARCHITECTURE.md`. Locally, sign in as `operator@qlab.dev`; in staging, use a
Google account in staging's allowlist. Step-by-step: `docs/runbook.md` →
"Frontend dev loop".

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
