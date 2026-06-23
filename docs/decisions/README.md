# Decision log

Lightweight architecture decision records (ADRs) for **cross-cutting choices whose
rationale isn't obvious from the code**. Detailed rationale for the scheduling model
and the build plan lives in `docs/ALGORITHM.md` and `docs/PLAN.md`; this folder
captures standalone decisions and their alternatives so they aren't re-litigated.

## Format

One file per decision, numbered `NNNN-short-title.md`. Each records:

- **Status** — Proposed / Accepted / Superseded (by #NNNN).
- **Context** — the forces at play.
- **Decision** — what we chose.
- **Consequences** — tradeoffs, including what we gave up.
- **Alternatives considered.**

## Index

- [0001 — Public site vs. data API are separate surfaces](0001-topology-public-site-vs-data-api.md)
- [0002 — CI/CD via GitHub Actions + Workload Identity Federation](0002-cicd-and-workload-identity.md)
- [0003 — The database enforces domain invariants (belt-and-suspenders)](0003-database-enforced-invariants.md)
- [0004 — Least-privilege Neon roles per access pattern](0004-least-privilege-database-roles.md)
- [0005 — Row-level security for tenant isolation](0005-row-level-security-tenant-isolation.md)
- [0006 — Request identity (principal) and lab scoping](0006-request-identity-and-lab-scoping.md)
- [0007 — Authentication: Firebase verification, the Auth emulator, and dev-login](0007-authentication-firebase-and-dev-login.md)
