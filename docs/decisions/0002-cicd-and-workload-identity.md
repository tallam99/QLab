# 0002 — CI/CD via GitHub Actions + Workload Identity Federation

**Status:** Accepted (2026-06-20)

## Context

Phase 3 needs both surfaces — the API (Cloud Run) and the PWA (Firebase Hosting) —
to deploy to **staging** and **production** from `main`, under three constraints:

- **$0/month** (Guiding constraint 1): GitHub Actions, Cloud Run, Artifact
  Registry, and Firebase Hosting all have free tiers that cover this.
- **The local/cloud boundary** (Guiding constraint 2): Claude authors the
  pipeline but never runs a cloud command or approves a prod deploy.
- **No leaked credentials**: a long-lived service-account JSON key in GitHub
  secrets is the obvious approach and the one to avoid — it's a standing liability
  if the repo or a secret leaks.

## Decision

- **GitHub Actions**, two workflows: `ci.yml` (the merge gate — build/vet/test +
  secret scan + lint) and `deploy.yml` (CD on push to `main`). Deploy logic is a
  reusable workflow (`_deploy.yml`) invoked once per environment, so staging and
  prod run identical steps with environment-scoped values.
- **Workload Identity Federation (WIF)** for all cloud auth: GitHub's OIDC token
  is exchanged for short-lived GCP credentials, scoped to this repository. **No
  service-account keys** — including the Firebase Hosting deploy, which reuses the
  same WIF credentials via firebase-tools' ADC.
- **Promotion = GitHub Environments.** Staging deploys automatically on merge;
  production is a separate job gated by the `production` Environment's
  required-reviewers rule. The user is the approver.
- **Config via environment-scoped Actions variables**; the one real secret
  (`DATABASE_URL`) lives in **GCP Secret Manager**, mounted into Cloud Run — never
  in GitHub.

## Consequences

- No long-lived key exists to leak or rotate; access is repo-scoped and
  per-run-ephemeral. The one-time WIF setup is more involved than pasting a key —
  worth it, and documented step-by-step in `docs/deploy.md`.
- Staging and prod share one tested code path (`_deploy.yml`); they can only drift
  via their variables, not their logic.
- The prod gate is enforced by the platform (a protected Environment), not by
  convention — it cannot be bypassed by a workflow edit alone.
- **Tradeoff / known overlap:** the API connects to its database on boot, so a
  Cloud Run revision only goes healthy once a reachable database exists. Phase 3's
  backend deploy therefore depends on a Neon instance (no schema needed — just a
  connection), nudging Phase 5's database *creation* slightly earlier. Documented
  in `docs/deploy.md`. The frontend deploy has no such dependency.

## Alternatives considered

- **Service-account JSON key in GitHub secrets.** Simplest to set up; a standing
  credential that outlives any single run and leaks as plaintext if a secret or
  the repo is compromised. Rejected — WIF removes the secret entirely.
- **Tag-triggered prod deploys** (push a `v*` tag to ship prod). Works, but the
  approval lives in whoever can push tags rather than in an auditable platform
  gate with a named reviewer. The Environment gate is clearer and matches the
  "user approves prod" boundary. Rejected for now (easy to add later).
- **Single combined deploy workflow** (no reusable file). More duplication and
  two places to keep in sync. Rejected in favor of `_deploy.yml`.
- **Cloud Build** for image builds. Fine and free, but building in the runner and
  pushing to Artifact Registry is one fewer moving part and reuses the existing
  `backend/Dockerfile` (root build context) directly. Chose the runner build.
