# 0004 — Least-privilege Neon roles per access pattern

**Status:** Accepted (2026-06-21)

## Context

Through Phase 3 the API connected to Neon with the branch's default/owner role,
and manual access (DBeaver) would use the same string. That conflates three very
different needs — the service's runtime access, a human poking around read-only,
and a human making an ad-hoc write — behind one all-powerful credential, and it
means a human needs the owner password to connect. Neon has **no GCP IAM database
auth and no cloud-sql-proxy** (those are Cloud SQL features), so a Neon connection
is always a password-bearing string; the only lever we have is *which role* the
string authenticates as and *where the string lives*.

## Decision

Per Neon branch (staging, production), provision **dedicated least-privilege roles**,
none of them the owner:

- **`qlab_app`** — the service's runtime role: `SELECT/INSERT/UPDATE/DELETE` on the
  application tables, no DDL, no superuser. The Cloud Run service connects as this.
  Its string is the existing `db-url-<env>` secret (so the deploy wiring is
  unchanged — only the secret's *value* becomes the app role's string).
- **`qlab_human_rw`** — ad-hoc human read-write (e.g. a manual fix via DBeaver).
- **`qlab_human_ro`** — human read-only inspection.

Each role's connection string is stored as its **own** Secret Manager secret
(`db-url-<env>` for the app; `db-url-<env>-readwrite` / `-readonly` for humans).
The Neon **owner** credential is reserved for schema changes (migrations): it lives
in a `db-url-<env>-migrator` secret readable **only by the CI deployer**, which runs
migrations before each deploy (see `_deploy.yml` / decision in `docs/deploy.md`). The
owner credential is never an everyday or laptop credential.

Humans fetch a string on demand via `mage dbStringStaging` / `mage dbStringProd`,
which read the read-write human secret using the operator's own `gcloud` auth and
print it for pasting into DBeaver — so the credential lives only in Secret Manager,
never persisted in the repo, and is never the owner password. (Read-only-specific
targets can be added later.) These targets are **user-run only**: per the project
boundary, Claude never authenticates to or invokes `gcloud`.

The role/secret provisioning steps live in `docs/deploy.md` and are run by the
user in Neon + GCP; Claude drafts but never executes them.

## Consequences

- A leaked human or app credential can't do DDL or act as owner; blast radius is
  scoped to that role's grants.
- More moving parts: three roles and up to three secrets per environment, plus
  `ALTER DEFAULT PRIVILEGES` so future migration-created tables are reachable by the
  non-owner roles. Documented in the deploy runbook.
- Manual access is "fetch fresh from Secret Manager, paste, don't save," which is a
  small friction traded for not having a long-lived owner password in a desktop client.
- **Local dev is exempt:** the Compose Postgres uses its single dev superuser — the
  role split is a cloud concern; mirroring it locally would buy nothing.

## Alternatives considered

- **One owner credential everywhere** (the Phase 3 state). Simplest, but every
  connection is all-powerful and humans need the owner password. Rejected.
- **GCP IAM database auth / a proxy** (as with Cloud SQL). Not available for Neon.
- **A single shared human role.** Loses the read-only vs read-write distinction that
  makes safe inspection the default; both roles are cheap, so we keep both.
