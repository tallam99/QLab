# 0001 — Public site vs. data API are separate surfaces

**Status:** Accepted (2026-06-19)

## Context

The data API should not be a publicly-browsable surface. But QLab is a PWA used on
phones, so the API host is necessarily reachable over the internet — true network
isolation (internal-only ingress) would break the app. "Not public," then, means
*auth on every data endpoint + no public/marketing content co-mingled with the API.*

We also want a simple deploy story for a frontend-novice maintainer, and a $0/month
cost ceiling.

## Decision

Split the two surfaces physically:

- **Public PWA** — static React build on **Firebase Hosting** (`qlab.app`): CDN,
  free HTTPS, atomic deploys, one-command rollback. No data, no secrets.
- **Data API** — Go **Connect-RPC on Cloud Run** (`api.qlab.app`): every endpoint
  requires a verified Firebase JWT, scoped to the caller's lab. No HTML/marketing.

The two are different origins; the API is configured for **CORS** from the Hosting
origin (also covers the SSE `EventSource` stream).

## Consequences

- The API host carries zero public content — the separation is physical, not just
  middleware discipline.
- Firebase is already the auth provider, so Hosting adds no new vendor and stays free.
- Simpler for a frontend novice than embedding the React build in the Go binary: no
  `embed.FS`, no hand-written SPA fallback, no backend rebuild for a frontend change.
- **Tradeoff:** cross-origin means CORS must be configured (one-time, well-trodden).

## Alternatives considered

- **One container embedding React in the Go binary** (the original sketch). One
  artifact/origin (no CORS), but co-mingles the surfaces and burdens the maintainer
  with `embed.FS` + SPA fallback + full rebuilds for CSS tweaks. Rejected.
- **Two Cloud Run services** (static site + API). Physical separation without Firebase
  Hosting, but you'd build and maintain a static-file-serving container instead of
  using a free purpose-built CDN. Strictly more work, no gain. Rejected.
