# frontend

The React + TypeScript PWA, deployed to Firebase Hosting.

> **Status:** not yet scaffolded — created in Phase 9 (see `docs/PLAN.md`).

## Stack (planned)

Vite · React · TypeScript · Tailwind · Connect-Query (generated Connect client) ·
Firebase JS SDK (Google sign-in) · vite-plugin-pwa · Biome · Vitest + RTL · Playwright.

## Conventions

- API access uses the **generated** Connect TS client (from `proto/`) — no hand-
  written `fetch` calls.
- The app is a static bundle served from a CDN; it talks to the Cloud Run API
  cross-origin (CORS). There is no frontend server running our code.

See `docs/PLAN.md` (Phases 9, 10) and `docs/ARCHITECTURE.md`.
