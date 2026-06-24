# 0007 — Authentication: Firebase verification, the Auth emulator, and dev-login

**Status:** Accepted (2026-06-22). The **dev-login endpoint** described here was
superseded by the secret-gated operator surface in **decision 0008** (the
token-minting primitive moved behind `qlab.dev.v1.DevService.MintToken`); the rest
— token verification, the emulator-for-local-dev choice, auth-as-Connect-interceptor,
invite-only provisioning — stands.

Builds on decision 0006 (the principal + lab scoping), which fixed *what* identity
the handlers see. This records *how* that identity is established: token
verification, how it is exercised locally, and the development login path.

## Context

Every data RPC requires a verified Firebase identity (topology decision 0001).
Phase 8 must turn the dev-header stand-in into real verification without changing
handlers (decision 0006 left the `principal.Principal` seam for exactly this), and
do it under the project's hard local/cloud boundary: **Claude operates local only
and never touches a real Firebase project.** So "real auth" has to be fully
exercisable offline, and the development conveniences the plan calls for
(impersonate a seeded user without the Google OAuth dance) must be impossible to
ship to production.

## Decision

- **Verify Firebase ID tokens with the Firebase Admin SDK**, behind a small
  `auth.TokenVerifier` interface (impl in `auth/firebaseauth`). One verify path
  serves every environment; the project id (per-environment) is the audience check.
- **Local dev and the integration suite run against the Firebase Auth emulator**,
  not a hand-written fake verifier. The same Admin SDK, pointed at the emulator via
  `FIREBASE_AUTH_EMULATOR_HOST`, skips only the signature check — so the genuine
  verify → claims → provision path is what tests exercise. The emulator is a
  compose service locally and a background process in CI (mirroring how Postgres is
  a compose service locally and a service container in CI).
- **Authentication is a Connect interceptor**, not chi middleware. It reads
  `Authorization: Bearer` + the selected-lab header, resolves the user, and attaches
  the principal — yielding native Connect codes: a bad/expired/absent token is
  `Unauthenticated`; a valid token whose email was never invited is
  `PermissionDenied` (distinct, so the UI can say "you're not invited" rather than
  "log in again"). Health and dev-login are plain routes outside this interceptor.
- **Provisioning is invite-only, by verified email.** First login links the
  Firebase uid (and name) onto the pre-created `users` row matched by the verified
  email; there is no self-service signup. The `users` table is global (not
  RLS-scoped), so this lookup runs before any lab is chosen.
- **The dev-login endpoint mints a usable bearer token in one call.** Given a
  seeded user's email it mints a custom token and exchanges it (via the Identity
  Toolkit `signInWithCustomToken` endpoint, which the emulator also serves) for an
  ID token — so a single `POST /devlogin` returns a token usable directly in
  `curl`/Yaak/the frontend. It is mounted **only outside production**; the server
  **refuses to boot** if it is enabled in production, and the config **refuses the
  emulator** in production. Both guards are test-asserted.

## Consequences

- The whole auth path is testable offline, with high fidelity — no mock-vs-real
  drift. The cost is one more local/CI dependency (the emulator: Node + a JRE +
  firebase-tools), accepted for that fidelity.
- Handlers are unchanged from Phase 7: the interceptor populates the same
  principal the dev-header middleware used to. Swapping the verifier (or pointing it
  at real Firebase) is a wiring change, not a handler change.
- The dev-login / emulator surfaces are the most dangerous things in the codebase
  if shipped to prod, so they are guarded in two independent layers (config refuses
  the emulator in prod; the server refuses to mount dev-login in prod) and asserted
  by tests — defense in depth, matching decision 0006's posture.
- Real staging/prod still require the user to enable Google sign-in and provide the
  project id + (for dev-login in staging) the web API key — the cloud half Claude
  does not touch.

## Alternatives considered

- **A fake `TokenVerifier` for local/tests instead of the emulator.** Simpler (no
  emulator infra) but tests a stub, not the real verify/claims path; the emulator's
  fidelity was judged worth the infra. The interface still exists, so a fake remains
  available for narrow unit tests.
- **Auth as chi middleware (like the Phase-7 stand-in).** Rejected: an HTTP error
  written before Connect maps to coarse codes (a 400 becomes `internal`), losing the
  Unauthenticated-vs-PermissionDenied distinction the invite-only UX needs. An
  interceptor emits native Connect codes.
- **Dev-login returns a custom token (client exchanges it).** Rejected for
  ergonomics: it would force every caller (curl, Yaak, tests) through a second
  round trip. Exchanging server-side returns a ready-to-use token in one call and
  still works against real Firebase.
- **Self-service signup (provision any verified Google account).** Rejected: the
  product is invite-only (PLAN Phase 8); membership is granted by a lab head, so an
  un-invited but validly-authenticated caller is refused.
