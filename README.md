# Quelab

Lab equipment scheduling PWA with a live queue and a **multi-bench scheduling
engine** that continuously re-flows the queue when experiments run over, finish
early, or get cancelled. Built initially for a ~15-person biology lab sharing
ventilation hoods.

> **Status:** early development — Phase 0 (foundations). Most of the system isn't
> built yet; see the roadmap below.

## Documentation

All project docs live in [`docs/`](docs/):

| Doc | What it is |
|-----|------------|
| [`docs/PLAN.md`](docs/PLAN.md) | The engineering roadmap — phased build plan with exit criteria. |
| [`docs/ALGORITHM.md`](docs/ALGORITHM.md) | The scheduling-engine spec — **read before touching scheduling logic.** |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | The system map — components, surfaces, environments. |
| [`docs/runbook.md`](docs/runbook.md) | How to run and debug the local stack. |
| [`docs/decisions/`](docs/decisions/) | Decision log (ADRs) for cross-cutting choices. |
| [`CLAUDE.md`](CLAUDE.md) | Orientation for a fresh Claude Code session. |

## Tech stack (summary)

- **Frontend:** React + TypeScript PWA (Vite), deployed to Firebase Hosting.
- **Backend:** Go Connect-RPC API on Google Cloud Run.
- **Contract:** Protobuf via Connect + buf (shared Go + TypeScript types).
- **Database:** Neon (serverless Postgres).
- **Auth:** Firebase Auth (Login with Google).
- **Notifications:** transactional-outbox email (Resend/SendGrid); modular for SMS/push.

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for detail and
[`docs/PLAN.md`](docs/PLAN.md) for the build order.

## Local development

The local stack (Docker Compose + Postgres, driven by `mage` targets) lands in
Phase 2 — see [`docs/runbook.md`](docs/runbook.md). Until then there is nothing to
run locally.

## Cost

The entire stack is designed to run within free tiers — **$0/month** aside from
tooling subscriptions.
