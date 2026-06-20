# frontend

The React + TypeScript PWA, deployed to Firebase Hosting.

> **Status:** Phase 3 placeholder. A minimal static **hello-world** page lives in
> `public/`; the CD pipeline deploys it to Firebase Hosting to prove the
> two-surface topology end-to-end (it fetches the API `/healthq` cross-origin, so
> a green status confirms CORS works). The real Vite/React/Tailwind PWA replaces
> it in Phase 9.

## Hello-world build (Phase 3)

`build.sh` copies `public/` to `dist/` and injects the target environment's API
base URL into the page:

    API_BASE_URL=https://<cloud-run-url> frontend/build.sh   # -> frontend/dist/

`firebase.json` (repo root) serves `frontend/dist` with an SPA rewrite. The CD
workflow runs this build and deploys `dist/` (see `docs/deploy.md`). Phase 9
swaps `build.sh` for `vite build` (same `dist/` output, so the pipeline is
stable).

## Stack (planned)

Vite · React · TypeScript · Tailwind · Connect-Query (generated Connect client) ·
Firebase JS SDK (Google sign-in) · vite-plugin-pwa · Biome · Vitest + RTL · Playwright.

## Conventions

- API access uses the **generated** Connect TS client (from `proto/`) — no hand-
  written `fetch` calls.
- The app is a static bundle served from a CDN; it talks to the Cloud Run API
  cross-origin (CORS). There is no frontend server running our code.

See `docs/PLAN.md` (Phases 9, 10) and `docs/ARCHITECTURE.md`.
