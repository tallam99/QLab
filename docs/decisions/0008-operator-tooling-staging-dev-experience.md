# 0008 — Operator tooling for the staging dev experience

**Status:** Accepted (2026-06-23)

Builds on decision 0006 (the principal + lab scoping). Real Firebase ID-token
verification — the Admin SDK behind the `auth.TokenVerifier` seam, an auth Connect
interceptor populating the principal, and invite-only first-login provisioning by
verified email — is implemented and clear from the code. The one non-obvious auth
choice is recorded here: **local and CI exercise the real verify→provision path
against the Firebase Auth emulator** (the same Admin SDK pointed at the emulator via
`FIREBASE_AUTH_EMULATOR_HOST`, which skips only the signature check), rather than a
fake `TokenVerifier` — higher fidelity at the cost of one local dependency (Node + a
JRE + firebase-tools; a compose service locally, a background process in CI). Config
refuses to boot if the emulator is configured in production. This decision then
covers the staging dev tooling built on top, which replaces an earlier
unauthenticated dev-login endpoint.

## Context

Developing and demoing QLab needs a fast, low-friction staging workflow: spin up a
fresh lab with several people and some equipment, then act as any of those people
fluidly (no juggling many browser logins), with labs serving as isolated per-feature
workspaces. The previous dev-login endpoint solved only "act as one user," and it
was **unauthenticated** — anyone who could reach the URL could mint a token. The
hard requirement: **none of this — provisioning, impersonation, indiscriminate
lab/user switching — may exist in production**, ever.

## Decision

A staging/local-only **operator surface** (`qlab.dev.v1.DevService`): `ProvisionLab`,
`MintToken`, `ListLabs`, `GetLab` (full state export), `TeardownLab`. It is the one
privileged primitive — "create a workspace, then act as anyone in it" — that the CLI
now and an in-app dev switcher later (Phase 9) both build on.

- **A separate Connect service, not part of `qlab.v1`.** The operator capability
  lives in its own package (`internal/devapi` over `internal/services/operator`) and
  is a distinct service. The production binary simply never mounts it — there is no
  operator code path in prod to disable.
- **Gated by an operator secret.** Every call requires `X-QLab-Operator-Secret`
  (constant-time compared). The secret lives only in staging's Secret Manager; the
  CLI fetches it. A browser dev switcher (Phase 9) will instead gate on the
  operator's real Google login against a staging-only allowlist — a second front
  door to the same capability, never a secret in the browser.
- **Runs over an elevated, RLS-bypassing DB connection** (`OPERATOR_DATABASE_URL`),
  because provisioning a *new* lab and listing *all* workspaces are inherently
  cross-tenant — the kind of thing the per-request, RLS-scoped store deliberately
  cannot do. `store.OperatorStore` is a second interface beside `store.Store`,
  implemented by the same type over a different connection.
- **Provisioning goes through the app, not raw SQL.** `ProvisionLab` creates the
  lab, a head + members, and a pool with resources in one transaction, returning the
  roster. Users are created unlinked; `MintToken` (or a real first login) links the
  Firebase identity by verified email — exercising the real provisioning path.
- **Production safety, four independent layers.** (1) The operator package is mounted
  only when `config.OperatorEnabled()` (env ≠ production AND a secret is set), so the
  prod binary never references it; (2) config refuses to even load if any operator env
  (`OPERATOR_SECRET`/`OPERATOR_DATABASE_URL`) is set in production — the process exits;
  (3) "operator" is **not** a product role (`lab_role`) — the product model has no
  switch-labs-at-will path; (4) a test asserts config rejects operator env in
  production. Any one failing is caught by the next.

## Consequences

- The staging dev loop is: provision a per-feature workspace, mint a token for any
  user, act as them — scriptable now (CLI/curl/tests), and the same endpoints back a
  future in-app switcher. `GetLab` doubles as the `lab_id`-scoped state-export from
  the observability convention.
- The open dev-login endpoint is gone; token minting is now behind the operator
  secret, closing the "anyone with the URL gets a token" exposure even in staging.
- The elevated connection is itself sensitive, so it is gated identically (secret +
  non-prod + separate service) and provisioned only in staging.
- The shared secret grants full control of staging to anyone holding it. Acceptable
  because staging carries only demo data and prod is structurally excluded; it can be
  upgraded to per-developer GCP IAM/OIDC later without changing the surface.
- Teardown deletes a lab and its cascade; the global `users` rows it created are left
  (they may belong to other labs) — harmless in staging.

## Alternatives considered

- **A standalone CLI binary as the primary client.** Rejected as the *interface*: the
  endpoints are the real primitive; a binary would duplicate them and be superseded by
  the in-app switcher. Automation calls the endpoints directly; humans get the
  (Phase 9) widget.
- **Plain HTTP/JSON operator endpoints.** Workable, but a typed Connect service keeps
  the proto-first discipline and gives the future widget a generated TS client.
- **Provision via a client-side seed script** (Neon RW string + Admin SDK). Rejected:
  bypasses app invariants and duplicates user-creation logic; the endpoint reuses one
  consistent path.
- **Gate dev-login/operator with only an env flag (no separate service).** Rejected:
  a flag deep in a shared handler is weaker than a whole service the prod binary never
  mounts. Structural exclusion + credential absence is the stronger guarantee the
  "never in prod" requirement demands.
