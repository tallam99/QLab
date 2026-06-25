# QLab — Deploy & CI/CD

> How the two surfaces ship: the **API** to **Cloud Run** and the **PWA** to
> **Firebase Hosting**, for **staging** and **production**, via GitHub Actions.
>
> **Boundary (see `CLAUDE.md`):** Claude authors everything in this file but
> **runs none of it** — every `gcloud`/`firebase`/`neon` command and the prod
> approval are **yours**. Claude never authenticates to or invokes a cloud CLI.

## Pipeline at a glance

| Workflow | Trigger | What it does |
|----------|---------|--------------|
| `.github/workflows/ci.yml` | every PR; and via `deploy.yml` on push to `main` | `go build`/`vet` + `mage testUnit` (Go unit tests), `mage testSecurity` (Yaak secret scan + its tests), `golangci-lint`. The merge gate. |
| `.github/workflows/deploy.yml` | push to `main` (i.e. after merge) | Run `ci.yml` as a gating job, then (only if green) deploy both surfaces to **staging** automatically, then to **production** behind a manual approval. Calls the reusable `_deploy.yml` once per environment. |

`_deploy.yml` for one environment: build the backend image → push to Artifact
Registry → **run database migrations** (`mage migrate` against the migrator secret,
before the new revision) → deploy to Cloud Run → **build the frontend** (`vite build`,
with the live Cloud Run URL and the Firebase web config injected at build time) →
deploy to Firebase Hosting.

**Auth is Workload Identity Federation (WIF), end to end** — GitHub mints a
short-lived OIDC token that GCP trusts, so there is **no long-lived key anywhere**
(not in the repo, not in GitHub secrets). `gcloud` uses it natively for Cloud Run,
Artifact Registry, and Secret Manager.

Firebase Hosting needs a small workaround: firebase-tools' CLI can't authenticate
non-interactively without a `firebase login:ci` refresh token — it ignores both the
WIF ADC and service-account keys — so we **don't use firebase-tools at all**. Instead
`scripts/deploy-hosting.sh` deploys `frontend/dist` straight to the **Hosting REST
API** using the deploy SA's WIF access token (the SA carries `firebasehosting.admin`).
Keyless, and with no firebase-tools dependency to break.

## Promotion strategy

Merge to `main` → **staging deploys automatically** (both surfaces) → the
`production` job waits on the `production` GitHub **Environment**, which has a
**required-reviewers** rule → **you approve** → production deploys. Staging must
succeed before production is even offered (`needs: staging`). Per the project
boundary, **only you approve prod.**

---

## ⚠️ Sequencing note: the backend needs a reachable database

The API **requires `DATABASE_URL` and connects to Postgres on boot** (Phase 2
design — there is no DB-less mode; the process exits non-zero if the DB is
unreachable, and Cloud Run then marks the revision unhealthy). So:

- The **frontend** Hosting deploy has no such dependency and satisfies its half
  of the Phase 3 exit criteria immediately.
- The **backend** Cloud Run deploy will only go green once a **reachable
  database** exists and `DATABASE_URL` resolves to it.

The schema/migrations (Phase 5) are **not** required for this — `/healthq` and
`/readyq` only need a successful connection, not tables. Two options:

1. **Recommended:** create a bare **Neon** project/branch now (a few minutes;
   pull just the *creation* part of Phase 5 forward), put its connection string
   in Secret Manager as `DATABASE_URL`, and the backend deploy goes green.
2. Or deploy only the frontend now and let the backend's first green deploy land
   with Phase 5. The pipeline is identical either way.

This is the one place Phase 3 and Phase 5 overlap; flagging it so a red backend
deploy isn't mistaken for a pipeline bug.

---

## Database setup (Neon) — you run these

> **Boundary:** Neon stays **user-driven** — the GCP exception does not extend to
> it. Claude drafts these steps; you run them in the Neon console.

### What Neon "branches" are (the part that's confusing)

Neon is **serverless Postgres**: a managed Postgres that scales to zero when idle
(so it costs nothing while unused) and wakes on the next connection.

A Neon **branch** is a **copy-on-write clone of your database** — think "a git
branch, but for the data and schema." Creating one is near-instant and basically
free: the new branch *shares* its parent's stored data until you write to it, and
only the changes you make afterward take new space. Each branch is a fully
isolated Postgres database with **its own connection string**, so two branches can
hold different data and diverge without affecting each other.

Why we use them: instead of running two separate database servers for staging and
production (cost + ops), we keep **one Neon project with two branches** — one for
production, one for staging — giving two isolated databases at ~$0.

### Topology: `production` (the default branch) + a `staging` child

Keep the project's **default branch as `production`** and create **one `staging`
branch off it**. Two branches total — *don't* rename the default to `base` or add a
third root branch.

This is safe even though `staging` is nominally a "child" of `production`, because
**a branch is only a copy of its parent at the instant it's created — after that the
two are completely independent.** There's no ongoing sync; the parent link is used
by exactly one operation ("reset a branch from its parent"), which we never run. So:

- Right now `production` is empty (no schema yet), so `staging` is born empty too —
  nothing real is ever copied between them.
- `staging` then diverges on its own: its own schema (Phase 5 migrations) + seeded
  demo data, while `production` holds real data. They never cross.

(The symmetric "`base` root + two children" layout is also valid but buys no extra
isolation — branches are independent regardless — so it's not worth the extra idle
branch. A data-free `base` only becomes useful *later* if you add per-PR preview
branches and want to fork them from a schema-only source; you can create it then.)

### Steps

1. **Create a Neon project** (Neon console → New Project). Pick a region near your
   Cloud Run region (us-east1). It comes with one default branch (often named
   `main` or `production`) — **this is your production branch** (rename it to
   `production` if it isn't already).
2. **Create the `staging` branch** off it (console → Branches → New branch,
   parent = the default `production` branch, name = `staging`). You now have two
   independent databases.
3. **Copy each branch's connection string** (console → Connection Details → pick
   the branch → copy the **pooled** connection string; it ends with
   `?sslmode=require`). One string per branch.
4. **Store each string in the matching project's Secret Manager** `DATABASE_URL`
   (next section): the **staging** branch's string → the **staging** GCP project's
   secret; the **production** branch's string → the **production** project's secret.

Notes:
- The **schema/migrations land in Phase 5** — for Phase 3 a bare branch (no tables)
  is enough for `/healthq` and `/readyq`, which only need a successful *connection*.
- Neon scales to zero, so the first connection after an idle period has a cold
  start; the service's boot retry rides it out, and the Phase 11 weekly cron
  doubles as a keep-alive.

---

## Database roles & access (Phase 5)

> ✅ **Done (2026-06-21)** for both `qlab-staging` and `qlab-production`, by Claude
> under a one-time exception to the local/cloud boundary (the user explicitly
> authorized the role + Secret Manager setup). Each branch uses a **dedicated
> database** — `qlab-staging` / `qlab-production` (not the default `neondb`); all
> connection strings point at it. Created on each Neon branch: roles
> `qlab_app` (NOSUPERUSER NOBYPASSRLS), `qlab_human_rw`, `qlab_human_ro`, with
> grants + `ALTER DEFAULT PRIVILEGES` so future migration-created tables are
> reachable. Secrets per project: `db-url-<env>` rotated to the `qlab_app` string;
> new `db-url-<env>-migrator` (the Neon owner string; deployer SA has
> `secretAccessor`), `db-url-<env>-readwrite`, `db-url-<env>-readonly`. The
> `DATABASE_MIGRATOR_SECRET` env variable is set for both GitHub Environments. All
> three roles were verified to authenticate via the pooled endpoint. The commands
> below are kept for reference/recreation; Claude does not resume running cloud
> commands — this was a one-off.

The service must **not** connect as the Neon owner, and humans must **not** need the
owner password. Per branch (staging, production) create three least-privilege roles
and give each its own Secret Manager secret. The Neon **owner** is reserved for
migrations: its string lives in a separate `db-url-<env>-migrator` secret that only
the **CI deployer** can read (the pipeline runs migrations before each deploy).

### 1. Create the roles (run in Neon's SQL editor, on each branch)

```sql
-- App runtime role: DML only, no DDL, no superuser, and NOBYPASSRLS so the
-- row-level-security tenant isolation (decision 0005) actually binds it.
CREATE ROLE qlab_app       LOGIN PASSWORD '<generate-a-strong-password>' NOSUPERUSER NOBYPASSRLS;
-- Human ad-hoc read-write.
CREATE ROLE qlab_human_rw  LOGIN PASSWORD '<generate-a-strong-password>';
-- Human read-only inspection. Optionally grant BYPASSRLS for cross-lab visibility
-- (otherwise it, too, is RLS-bound and must set app.current_lab_id per session):
--   ALTER ROLE qlab_human_ro BYPASSRLS;
CREATE ROLE qlab_human_ro  LOGIN PASSWORD '<generate-a-strong-password>';

-- Grants on existing objects.
GRANT USAGE ON SCHEMA public TO qlab_app, qlab_human_rw, qlab_human_ro;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO qlab_app, qlab_human_rw;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO qlab_app, qlab_human_rw;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO qlab_human_ro;

-- Future tables (created by later migrations as the owner) are reachable too.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO qlab_app, qlab_human_rw;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO qlab_app, qlab_human_rw;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT SELECT ON TABLES TO qlab_human_ro;
```

### 2. Store each role's connection string in Secret Manager

Build each string from Neon's **pooled** host for that branch and the role's
password (`postgres://<role>:<password>@<pooled-host>/<db>?sslmode=require`).

| Secret (staging project / prod project) | Holds the string for |
|------------------------------------------|----------------------|
| `db-url-staging` / `db-url-production` | `qlab_app` — **the Cloud Run runtime** (existing secret; rotate its value to the app role) |
| `db-url-staging-migrator` / `db-url-production-migrator` | the Neon **owner** — used by **CI** to run migrations before each deploy |
| `db-url-staging-readwrite` / `db-url-production-readwrite` | `qlab_human_rw` — DBeaver ad-hoc writes |
| `db-url-staging-readonly` / `db-url-production-readonly` | `qlab_human_ro` — read-only inspection |

```sh
# Rotate the runtime secret to the app role (Cloud Run picks up :latest on next deploy):
printf '%s' 'postgres://qlab_app:PASSWORD@HOST/DB?sslmode=require' \
  | gcloud secrets versions add db-url-staging --data-file=- --project qlab-staging

# Migrator secret = the Neon owner string; ONLY the CI deployer SA may read it.
printf '%s' 'postgres://OWNER:PASSWORD@HOST/DB?sslmode=require' \
  | gcloud secrets create db-url-staging-migrator --data-file=- --project qlab-staging
gcloud secrets add-iam-policy-binding db-url-staging-migrator \
  --member="serviceAccount:qlab-deployer@qlab-staging.iam.gserviceaccount.com" \
  --role=roles/secretmanager.secretAccessor --project qlab-staging

# Create the human secrets and let the runtime SA stay out of them (humans use their
# own gcloud identity to read these; only the app secret is granted to qlab-api):
printf '%s' 'postgres://qlab_human_rw:PASSWORD@HOST/DB?sslmode=require' \
  | gcloud secrets create db-url-staging-readwrite --data-file=- --project qlab-staging
printf '%s' 'postgres://qlab_human_ro:PASSWORD@HOST/DB?sslmode=require' \
  | gcloud secrets create db-url-staging-readonly  --data-file=- --project qlab-staging
```

### 3. Connect with DBeaver

Fetch the string on demand (don't save the owner password anywhere):

```sh
mage dbStringStaging   # prints the staging human read-write string
mage dbStringProd      # prints the production human read-write string
```

These run `gcloud secrets versions access` as **your** logged-in identity and print
the string to paste into DBeaver. They are **user-run only** — Claude never invokes
`gcloud`. (A read-only variant target can be added later; for now read the
`*-readonly` secret directly if you want read-only.)

> **RLS in DBeaver:** the human roles are `NOBYPASSRLS`, so until you set a tenant
> context you'll see no lab-scoped rows. Either scope your session —
> `SELECT set_config('app.current_lab_id', '<lab-uuid>', false);` — or grant the
> read-only role `BYPASSRLS` (above) for cross-lab inspection. The migrator (owner)
> credential bypasses RLS entirely.

### Applying migrations to staging/prod

Migrations are **not** run against Neon from a local machine (by you or Claude).
The deploy pipeline (`_deploy.yml`) runs `mage migrate` against the target branch
**before** deploying the new Cloud Run revision, using the migrator (owner) secret
fetched via WIF — so the owner credential never touches a laptop. A migration
failure fails the deploy.

Because the old revision briefly runs against the new schema (migrate-then-deploy),
keep migrations **backward-compatible** with the currently-live code
(expand/contract): add columns/tables before the code uses them; drop only after no
live revision references them.

---

## Operator surface (staging only)

The operator tooling (`qlab.dev.v1.DevService`: provision workspaces, mint tokens to
act as seeded users — decision 0008) is enabled by three env vars on the **staging**
Cloud Run service, all sourced from Secret Manager. **None are set on production** —
the service refuses to boot if any is present there, and the surface is not mounted
in prod regardless.

- `OPERATOR_SECRET` — the gate every operator call must present.
- `OPERATOR_DATABASE_URL` — the operator's elevated, cross-tenant connection.
- `FIREBASE_WEB_API_KEY` — the staging Web API key the operator's `MintToken` uses to
  exchange a custom token for an ID token.

**Elevated DB connection — reuse the migrator (owner) credential.** The operator's
cross-tenant work (create a lab, list all workspaces) needs to bypass RLS. Neon has
no superuser, so a `BYPASSRLS` role is impossible — but the table *owner* already
bypasses RLS (our policies are `ENABLE`, not `FORCE`), and the migrator credential
(`db-url-staging-migrator`) *is* the owner. So `OPERATOR_DATABASE_URL` reuses that
secret — **no new Neon role**. The tradeoff: the staging runtime then holds the
owner (DDL-capable) credential. Acceptable for staging — gated by the operator
secret, demo data only, and entirely absent in production. (A dedicated operator role
+ an RLS-policy exemption is the stricter alternative if least-privilege matters
more; it costs a migration.)

All of this is **already done** by Claude under the boundary exception
(`qlab-staging`); it activates on the next staging deploy:
- `operator-secret` created (random) + runtime SA granted `secretAccessor`.
- runtime SA granted `roles/iam.serviceAccountTokenCreator` on itself (for `MintToken`
  custom-token signing).
- runtime SA granted `roles/firebaseauth.admin` (project-level) — `MintToken`'s
  `ensureUser` step creates/looks-up Firebase Auth users via the Admin SDK, which
  needs Auth-admin permission. Without it `MintToken` fails with `INSUFFICIENT_PERMISSION`
  (the emulator doesn't enforce IAM, so this only surfaces against real Firebase).
  Staging-only: production never mints tokens, only verifies them (public-key, no IAM).
- runtime SA granted `secretAccessor` on `db-url-staging-migrator` (the operator DB url).
- `firebase-web-api-key` secret created from the project's auto-created Firebase
  "Browser key" + runtime SA granted `secretAccessor`.
- `OPERATOR_SECRET` + `OPERATOR_DATABASE_URL` (= `DATABASE_MIGRATOR_SECRET`) +
  `FIREBASE_WEB_API_KEY` wired into the staging Cloud Run deploy (`_deploy.yml`,
  staging-only).

> If the Firebase **Browser key** ever gets API restrictions that exclude the
> Identity Toolkit API, `MintToken`'s server-side `signInWithCustomToken` exchange
> will fail; rotate `firebase-web-api-key` to an unrestricted (or Identity-Toolkit-
> allowed) key then.

Drive the surface with the curl flow in `docs/runbook.md` → "Operator surface",
pointing it at the staging API URL (retrieve the gate value with
`gcloud secrets versions access latest --secret=operator-secret --project qlab-staging`).

---

## Live schedule updates (Phase 10 — realtime listener) — you run these

> ⚠️ **Not yet configured in the cloud.** Until the secret + variable below exist, the
> app runs fine but pushes **no cross-client live updates** in staging/prod (each user's
> own actions still update immediately; another user's don't until they reload). Local
> dev needs none of this.

Live schedule updates use Postgres `LISTEN`/`NOTIFY` (decision 0010). `LISTEN` needs a
**session-pinned** connection, which Neon's **pooled** endpoint (PgBouncer, transaction
mode) does **not** provide — on the pooled host `LISTEN` runs but notifications never
arrive. So the listener uses a dedicated connection to the branch's **direct (unpooled)**
endpoint, configured by `SCHEDULE_LISTENER_DATABASE_URL`.

To enable it per environment:

1. **Build the direct connection string** for the `qlab_app` role: take the existing
   `db-url-<env>` value and **remove `-pooler`** from the host (Neon's direct host is the
   pooled host minus that suffix). Same role, same database, `sslmode=require`.
2. **Store it** in a new Secret Manager secret and grant the runtime SA access:
   ```bash
   printf '%s' 'postgres://qlab_app:<password>@<direct-host>/<db>?sslmode=require' \
     | gcloud secrets create db-url-staging-listener --data-file=- --project qlab-staging
   gcloud secrets add-iam-policy-binding db-url-staging-listener \
     --member="serviceAccount:qlab-api@qlab-staging.iam.gserviceaccount.com" \
     --role=roles/secretAccessor --project qlab-staging
   ```
3. **Point the deploy at it** with an environment-scoped Actions variable (Settings →
   Environments → staging/production → Variables): set **`SCHEDULE_LISTENER_SECRET`** =
   `db-url-<env>-listener`. `_deploy.yml` attaches it as `SCHEDULE_LISTENER_DATABASE_URL`
   only when the variable is set, so the deploy never breaks before the secret exists.

The Cloud Run request timeout is already raised to `--timeout=3600` in `_deploy.yml` so
the SSE stream can stay open; no other change is needed.

---

## Cloud Trace (Phase 7.5 — tracing)

Tracing exports to **Google Cloud Trace** in staging/prod (stdout locally), chosen by
`QLAB_ENV` — which `_deploy.yml` already sets on Cloud Run. The exporter auto-detects
the project from the runtime SA's metadata, so **no app config, env var, or deploy
change is needed.**

> ✅ **Done (2026-06-23)** for both `qlab-staging` and `qlab-production`, by Claude
> under a one-time exception to the local/cloud boundary: `cloudtrace.googleapis.com`
> enabled and the `qlab-api` runtime SA granted `roles/cloudtrace.agent` in each. It
> activates on the next deploy. Commands kept below for reference/recreation.

```sh
# 1. Enable the Cloud Trace API.
gcloud services enable cloudtrace.googleapis.com --project "$PROJECT_ID"

# 2. Let the runtime SA write trace spans (cloudtrace.traces.patch).
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:qlab-api@${PROJECT_ID}.iam.gserviceaccount.com" \
  --role=roles/cloudtrace.agent
```

Without these the service still runs normally — tracing is never load-bearing (an
exporter that can't be built falls back to a no-op provider; batch-export failures are
logged, not fatal) — traces simply wouldn't appear in the Cloud Trace console.

---

## One-time GCP setup

> ✅ **Done (2026-06-20)** for both `qlab-staging` and `qlab-production`, by Claude
> under a one-time exception to the local/cloud boundary. Region: **us-east1**.
> Created in each project: Artifact Registry repo `qlab`; service accounts
> `qlab-deployer` (CI, 4 roles) and `qlab-api` (runtime); WIF pool/provider
> `github` scoped to `tallam99/QLab` + the deployer binding. The DB secrets
> (`db-url-staging`/`db-url-production`) and runtime-SA access are also done
> (step 4), and the Firebase Hosting default sites already exist. The commands are
> kept below for reference and recreation. All cloud setup is complete; what's left
> is to merge + approve (see "What's left" below). Claude does **not** resume
> running cloud commands; this was a one-off.

Done **twice** — once per project (`staging`, then `production`), with the
variables below set per session.

```sh
# --- per environment: fill these in ---
export PROJECT_ID="qlab-staging"          # your real GCP/Firebase project id
export REGION="us-east1"                  # Cloud Run + Artifact Registry region (matches the Neon DB region)
export REPO="github.com/tallam99/QLab"    # the GitHub repo WIF will trust
export AR_REPO="qlab"                     # Artifact Registry repo name
export PROJECT_NUMBER="$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')"
```

### 1. Enable APIs

```sh
gcloud services enable \
  run.googleapis.com \
  artifactregistry.googleapis.com \
  secretmanager.googleapis.com \
  iamcredentials.googleapis.com \
  iam.googleapis.com \
  sts.googleapis.com \
  firebasehosting.googleapis.com \
  --project "$PROJECT_ID"
```

(`iam` + `sts` back the Workload Identity Federation token exchange in step 5.)

### 2. Artifact Registry (Docker repo Cloud Run pulls from)

```sh
gcloud artifacts repositories create "$AR_REPO" \
  --repository-format=docker --location="$REGION" \
  --project "$PROJECT_ID"
```

### 3. Service accounts

Two SAs: one GitHub **impersonates to deploy**, one the **service runs as**.

```sh
# Deploy SA (assumed by GitHub Actions via WIF)
gcloud iam service-accounts create qlab-deployer \
  --display-name="QLab CI deployer" --project "$PROJECT_ID"
export DEPLOY_SA="qlab-deployer@${PROJECT_ID}.iam.gserviceaccount.com"

# Runtime SA (the identity the Cloud Run service runs as)
gcloud iam service-accounts create qlab-api \
  --display-name="QLab API runtime" --project "$PROJECT_ID"
export RUNTIME_SA="qlab-api@${PROJECT_ID}.iam.gserviceaccount.com"
```

Grant the **deploy SA** what it needs to ship both surfaces:

```sh
for ROLE in roles/run.admin roles/artifactregistry.writer \
            roles/iam.serviceAccountUser roles/firebasehosting.admin; do
  gcloud projects add-iam-policy-binding "$PROJECT_ID" \
    --member="serviceAccount:${DEPLOY_SA}" --role="$ROLE"
done
```

(`iam.serviceAccountUser` lets the deployer "act as" the runtime SA when deploying
the service.) `firebasehosting.admin` lets `scripts/deploy-hosting.sh` push to the
Hosting REST API with the deploy SA's WIF access token — no firebase-tools, no key.

### 4. Secret Manager — the database URL secret

> ✅ **Done.** The secrets exist and the runtime SAs can read them: `db-url-staging`
> in `qlab-staging` and `db-url-production` in `qlab-production` (each holds the
> matching Neon branch string). The deploy maps the secret to the container's
> `DATABASE_URL` env var; the secret's per-environment *name* is the
> `DATABASE_SECRET` GitHub variable.

The secret name is arbitrary — what matters is the container env var. Store the
matching Neon **branch** connection string (staging branch → staging project,
production branch → prod) and let the **runtime SA** read it:

```sh
# names used here: db-url-staging (qlab-staging) / db-url-production (qlab-production)
printf '%s' 'postgres://USER:PASSWORD@HOST/DB?sslmode=require' \
  | gcloud secrets create db-url-staging --data-file=- --project qlab-staging

gcloud secrets add-iam-policy-binding db-url-staging \
  --member="serviceAccount:qlab-api@qlab-staging.iam.gserviceaccount.com" \
  --role=roles/secretmanager.secretAccessor --project qlab-staging
```

Cloud Run mounts it via `--set-secrets DATABASE_URL=${DATABASE_SECRET}:latest`, so
the secret can be named anything as long as `DATABASE_SECRET` points at it. To
rotate, add a new secret *version* (`gcloud secrets versions add db-url-staging
--data-file=-`); `:latest` picks it up on the next deploy and the IAM grant carries
over.

### 5. Workload Identity Federation (no SA keys)

Let GitHub's OIDC token impersonate the deploy SA — scoped to **this repo only**.

```sh
gcloud iam workload-identity-pools create github \
  --location=global --display-name="GitHub Actions" --project "$PROJECT_ID"

gcloud iam workload-identity-pools providers create-oidc github \
  --location=global --workload-identity-pool=github \
  --display-name="GitHub OIDC" \
  --issuer-uri="https://token.actions.githubusercontent.com" \
  --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository" \
  --attribute-condition="assertion.repository=='${REPO#github.com/}'" \
  --project "$PROJECT_ID"

# Allow only this repo's workflows to impersonate the deploy SA.
gcloud iam service-accounts add-iam-policy-binding "$DEPLOY_SA" \
  --role=roles/iam.workloadIdentityUser \
  --member="principalSet://iam.googleapis.com/projects/${PROJECT_NUMBER}/locations/global/workloadIdentityPools/github/attribute.repository/${REPO#github.com/}" \
  --project "$PROJECT_ID"

# The value for the WIF_PROVIDER GitHub variable (print and copy it):
echo "projects/${PROJECT_NUMBER}/locations/global/workloadIdentityPools/github/providers/github"
```

### 6. Firebase Hosting

Each project is already a Firebase project (Phase 0). Ensure Hosting is
initialized for it (Firebase console → Hosting → Get started, or `firebase init
hosting` locally once). The default site is reachable at **two** origins —
`https://<PROJECT_ID>.web.app` and `https://<PROJECT_ID>.firebaseapp.com` — so
`CORS_ALLOWED_ORIGINS` (below) lists **both**; otherwise the page fails CORS when
loaded via the alias the user didn't allow.

> The deploy SA already has `roles/firebasehosting.admin` from step 3, so the CI
> Firebase deploy authenticates via WIF — no `firebase login:ci` token needed.

---

## GitHub setup (you run these)

### Environments + the prod gate

**Already created** (Phase 3): the `staging` and `production` Environments exist,
and `production` has a **required-reviewers** rule (the manual approval gate) with
`tallam99` as the reviewer. Staging auto-deploys; production waits for approval.

If you ever need to recreate them:

```sh
gh api -X PUT repos/tallam99/QLab/environments/staging
printf '{"reviewers":[{"type":"User","id":%s}]}' "$(gh api user -q .id)" \
  | gh api -X PUT repos/tallam99/QLab/environments/production --input -
```

What's left for you is to add each Environment's **variables** (below).

### Environment variables

> ✅ **Already set** for both environments (via `gh`, from the real values the GCP
> setup produced), **except `FIREBASE_WEB_API_KEY`** — that one is new (added with
> the Vite-build deploy swap) and must be set per environment before the frontend
> can sign in; see "What's left for you". Listed here for reference;
> `gh variable list --env staging` shows the current values. They are configuration,
> not secrets — WIF means none are credentials.

These are **environment-scoped Variables** (Settings → Environments → *name* →
Variables), one set for `staging` and one for `production`.

| Variable | Example (staging) | Notes |
|----------|-------------------|-------|
| `GCP_PROJECT_ID` | `qlab-staging` | |
| `GCP_REGION` | `us-east1` | Cloud Run + Artifact Registry region |
| `WIF_PROVIDER` | `projects/123…/locations/global/workloadIdentityPools/github/providers/github` | printed by step 5 |
| `DEPLOY_SERVICE_ACCOUNT` | `qlab-deployer@qlab-staging.iam.gserviceaccount.com` | |
| `CLOUD_RUN_RUNTIME_SA` | `qlab-api@qlab-staging.iam.gserviceaccount.com` | service runs as this |
| `ARTIFACT_REGISTRY_REPO` | `qlab` | |
| `CLOUD_RUN_SERVICE` | `api-staging` | `api-prod` for production |
| `CORS_ALLOWED_ORIGINS` | `https://qlab-staging.web.app,https://qlab-staging.firebaseapp.com` | the Hosting origin(s); comma-separate if more than one |
| `FIREBASE_PROJECT_ID` | `qlab-staging` | |
| `FIREBASE_WEB_API_KEY` | `AIza…` | the Firebase **Web "Browser key"** (Firebase console → Project settings → Your apps → Web app → `apiKey`). Public client config — it ships in the browser bundle — so a Variable, not a secret. Injected into `vite build` as `VITE_FIREBASE_API_KEY`. Staging's value is the same key already stored in the `firebase-web-api-key` Secret Manager secret (used server-side by `MintToken`). |
| `DATABASE_SECRET` | `db-url-staging` | name of the Secret Manager secret holding the app DB URL (`db-url-production` for prod) |
| `DATABASE_MIGRATOR_SECRET` | `db-url-staging-migrator` | name of the secret holding the migrator (owner) DB URL the pipeline runs migrations with (`db-url-production-migrator` for prod) |

The DB connection string itself is **not** here — only the *name* of its Secret
Manager secret is. The value lives in Secret Manager (step 4).

### Branch protection

On `main`: require the **CI** checks (`test`, `security`, `lint`) to pass,
squash-merge, auto-delete branches (Phase 0). This makes a green `ci.yml` the
gate for every merge. (`deploy.yml` *also* runs CI as a gating job before any
deploy, so even a direct push that bypasses a PR can't ship a red build — branch
protection and the deploy graph enforce the gate independently.)

---

## What's left for you

Most setup is **done**: GCP infra, the database secrets (+ runtime-SA access), the
Firebase Hosting default sites (`https://qlab-staging.web.app`,
`https://qlab-production.web.app`), and most GitHub config (Environments, prod
reviewer, variables). The Vite-build deploy swap adds one new requirement. To ship:

1. **Set `FIREBASE_WEB_API_KEY`** as an env-scoped Actions Variable in **both**
   `staging` and `production` (Settings → Environments → *name* → Variables). The
   value is that project's Firebase **Web "Browser key"** (Firebase console →
   Project settings → Your apps → **Web app** → `apiKey`; register a Web app first
   if the project has none). Staging's value = the key already in the
   `firebase-web-api-key` secret. The deploy **fails fast** if it's unset, so do
   this before merging. (Also confirm **Google sign-in** is enabled in each
   project's Authentication providers, and that `CORS_ALLOWED_ORIGINS` already lists
   the `.web.app` + `.firebaseapp.com` Hosting origins — it does, from Phase 3.)
2. **Merge this PR.** Staging deploys automatically.
3. **Approve the production deploy** when the `production` Environment prompts you.

Then verify (below) and paste the URLs back.

> Caveat: the only thing not verifiable until deploy is whether each Neon
> connection string is correct/reachable. If one is wrong, its backend Cloud Run
> revision will fail its health check (see the sequencing note up top) — the fix is
> a corrected secret value, not a pipeline change.

---

## Cloud Run health probes (recommended hardening)

The service is built to listen *before* dependencies initialize, so map the
probes accordingly (see `docs/runbook.md` → Health checks):

- **Startup probe → `/readyq`** — holds traffic until the DB connects; give it a
  timeout generous enough for a Neon cold start.
- **Liveness probe → `/healthq`** — restarts only a genuinely wedged process.

Cloud Run's default startup probe is a TCP check on the port, which our
listen-early server passes immediately; that's acceptable for Phase 3. To apply
the HTTP probes explicitly, set them in the console or via a service YAML
(`gcloud run services replace`):

```yaml
# api.run.yaml (excerpt) — startup gated on /readyq, liveness on /healthq
spec:
  template:
    spec:
      containers:
        - image: REGION-docker.pkg.dev/PROJECT/qlab/api:TAG
          startupProbe:
            httpGet: { path: /readyq }
            failureThreshold: 30
            periodSeconds: 2
          livenessProbe:
            httpGet: { path: /healthq }
            periodSeconds: 10
```

---

## Verify (Phase 3 exit criteria)

After a merge deploys staging (and after you approve prod):

```sh
# API up (liveness), and ready (DB reachable):
curl https://<cloud-run-url>/healthq   # {"status":"ok"}
curl https://<cloud-run-url>/readyq    # {"status":"ok"} once the DB connects

# PWA served, and reaching the API cross-origin (CORS):
open https://<PROJECT_ID>.web.app      # the real app: sign in with Google, then slots load
```

Signing in with Google and seeing the slots list load (after pasting a lab + pool,
or via the dev panel) proves the browser reached the Cloud Run API cross-origin —
i.e. **CORS works** and Firebase auth is wired — closing the topology tradeoff
(decision 0001). Paste the URLs back to Claude to confirm.

---

## Debugging staging (log queries)

Logs are structured JSON with a `request_id` on every line. Pull a slice and hand
it to Claude:

```sh
# Last 100 API log lines (staging):
gcloud logging read \
  'resource.type=cloud_run_revision AND resource.labels.service_name=api-staging' \
  --project qlab-staging --limit=100 --format=json

# One request's full story, by request id:
gcloud logging read \
  'resource.type=cloud_run_revision AND jsonPayload.request_id="<id>"' \
  --project qlab-staging --format=json
```
