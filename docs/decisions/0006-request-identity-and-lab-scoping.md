# 0006 — Request identity (principal) and lab scoping

**Status:** Accepted (2026-06-22)

## Context

Every data RPC acts as some user, within some lab, on some lab-scoped resource. The
API needs three things on each request: *who* the caller is, *which lab* they are
operating in, and the assurance that they may act there. Several forces shape how:

- **RLS is fail-closed (decision 0005).** A query with no `app.current_lab_id` set
  sees zero rows under the app role, so the lab must be known *before* the first read
  — it can't be discovered from a bare slot/pool id.
- **RLS checks lab match, not membership.** A policy only proves a row belongs to the
  session's lab; it does not prove the caller belongs to that lab. So membership is a
  separate, app-level question.
- **A user can belong to several labs**, so "the user" alone doesn't determine the
  lab; the UI's selected lab does.
- **Auth isn't built yet (Phase 8).** Phase 7 needs a stand-in that the real
  Firebase-JWT middleware can replace without touching handlers.

## Decision

- **A `principal.Principal{UserID, LabID}` (both `uuid.UUID`) carried in the request
  `context`.** It is the single representation of caller identity; handlers read it
  via `principal.FromContext`, never a raw credential. The lab is part of the
  principal (carried, not derived) precisely because RLS needs it up front.
- **One auth seam, swappable by phase.** Phase 7 populates the principal from dev
  headers (`X-QLab-User` / `X-QLab-Lab`, parsed to uuids; `httpmw.DevPrincipal`).
  Phase 8 replaces that middleware with Firebase-JWT verification (the lab from a
  selected-lab claim/header) producing the *same* `Principal`, so the scheduling
  service and handlers don't change.
- **Membership is checked in the app layer, over the main database.** An `authz`
  service (`services/authz`, interface + `v1`) answers `RequireMember` by reading
  `labs_users` through `store.Store`. It has **no database of its own**: membership
  lives in the application DB so the `slots → labs_users` composite foreign key
  (member-only booking, decision 0003) holds. RLS remains defense-in-depth beneath
  this check, not a replacement for it.
- **Defense in depth, three layers.** App-level membership check (authz) → app-level
  `labs_id` predicate on every query → RLS `app.current_lab_id` policy. A bug in one
  layer is caught by the next.

## Consequences

- Handlers stay auth-mechanism-agnostic; Phase 8 is a middleware swap, not a rewrite.
- The lab must be supplied by the caller (a header now, a claim later). A caller can
  *name* any lab, so the membership check is load-bearing — it, not RLS, is what
  stops cross-tenant access (a non-member naming another lab is denied; see the
  integration suite's cross-lab cases).
- Authorization policy has a home (`authz`) to grow into in Phase 8 (head-only
  actions, the dev-login guard) without leaking into scheduling. "Next-in-line" stays
  in scheduling — it is a queue-state rule, not an identity/role rule.
- The dev-header path is, like the future dev-login path, dangerous if it ever ships
  to prod; Phase 8 must compile/guard it off outside local/staging.

## Alternatives considered

- **A separate auth service with its own database.** Rejected: it makes the
  member-only-booking foreign key impossible and trades an integrity guarantee for
  distributed-data consistency problems, with no benefit at a 15-person scale.
- **Derive the lab from the target resource instead of carrying it.** Impossible
  under fail-closed RLS — resolving a resource's lab is itself a lab-scoped read.
- **Rely on RLS alone for access control.** Rejected: RLS checks lab match, not
  membership, so any authenticated user could read a lab they merely named.
- **Thread `userID`/`labID` as explicit params everywhere instead of a context
  principal.** More verbose at every layer; the hybrid (context at the transport
  edge, explicit `Principal` arg into the domain service) keeps the implicit part at
  the boundary only.
