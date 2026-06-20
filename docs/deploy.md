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
| `.github/workflows/ci.yml` | every PR + push to `main` | `go build`/`vet` + `mage testUnit` (Go unit tests), `mage testSecurity` (Yaak secret scan + its tests), `golangci-lint`. The merge gate. |
| `.github/workflows/deploy.yml` | push to `main` (i.e. after merge) | Deploy both surfaces to **staging** automatically, then to **production** behind a manual approval. Calls the reusable `_deploy.yml` once per environment. |

`_deploy.yml` for one environment: build the backend image → push to Artifact
Registry → deploy to Cloud Run → build the hello-world frontend with the live
Cloud Run URL injected → deploy to Firebase Hosting.

**Auth is Workload Identity Federation (WIF) end to end** — GitHub mints a
short-lived OIDC token that GCP trusts, so there is **no long-lived
service-account key** anywhere (not in the repo, not in GitHub secrets). The
Firebase deploy uses the same WIF credentials (firebase-tools reads the ADC the
auth step exports).

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

The schema/migrations (Phase 4) are **not** required for this — `/healthz` and
`/readyz` only need a successful connection, not tables. Two options:

1. **Recommended:** create a bare **Neon** project/branch now (a few minutes;
   pull just the *creation* part of Phase 4 forward), put its connection string
   in Secret Manager as `DATABASE_URL`, and the backend deploy goes green.
2. Or deploy only the frontend now and let the backend's first green deploy land
   with Phase 4. The pipeline is identical either way.

This is the one place Phase 3 and Phase 4 overlap; flagging it so a red backend
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
production, one for staging — giving two isolated databases at ~$0. (Later you can
also spin up a throwaway branch per PR to test a migration against real-shaped data,
then delete it — that's the payoff of copy-on-write.)

### Steps

1. **Create a Neon project** (Neon console → New Project). Pick a region near your
   Cloud Run region. It comes with one default branch (named `main` or
   `production`) — treat that as **production**.
2. **Create a second branch** named `staging` from it (console → Branches → New
   branch, parent = the default branch). You now have two isolated databases.
3. **Copy each branch's connection string** (console → Connection Details → pick
   the branch → copy the **pooled** connection string; it ends with
   `?sslmode=require`). One string per branch.
4. **Store each string in the matching project's Secret Manager** `DATABASE_URL`
   (next section): the **staging** branch's string → the **staging** GCP project's
   secret; the **production** branch's string → the **production** project's secret.

Notes:
- The **schema/migrations land in Phase 4** — for Phase 3 a bare branch (no tables)
  is enough for `/healthz` and `/readyz`, which only need a successful *connection*.
- Neon scales to zero, so the first connection after an idle period has a cold
  start; the service's boot retry rides it out, and the Phase 11 weekly cron
  doubles as a keep-alive.

---

## One-time GCP setup

> ✅ **Done (2026-06-20)** for both `qlab-staging` and `qlab-production`, by Claude
> under a one-time exception to the local/cloud boundary. Region: **us-east1**.
> Created in each project: Artifact Registry repo `qlab`; service accounts
> `qlab-deployer` (CI, 4 roles) and `qlab-api` (runtime); WIF pool/provider
> `github` scoped to `tallam99/QLab` + the deployer binding. The commands are kept
> below for reference and recreation. **Still pending (yours):** Neon + Secret
> Manager `DATABASE_URL` (step 4) and Firebase Hosting init — see "What's left"
> below. Claude does **not** resume running cloud commands; this was a one-off.

Done **twice** — once per project (`staging`, then `production`), with the
variables below set per session.

```sh
# --- per environment: fill these in ---
export PROJECT_ID="qlab-staging"          # your real GCP/Firebase project id
export REGION="us-central1"               # your chosen Cloud Run + Artifact Registry region
export REPO="github.com/tallam99/QLab"    # the GitHub repo WIF will trust
export AR_REPO="qlab"                     # Artifact Registry repo name
export RUN_SERVICE="api-staging"          # Cloud Run service name (api-prod for prod)
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
the service.)

### 4. Secret Manager — `DATABASE_URL`

Store the matching Neon **branch** connection string (from the Database setup
section above — staging branch for the staging project, production branch for prod)
and let the **runtime SA** read it.

```sh
printf '%s' 'postgres://USER:PASSWORD@HOST/DB?sslmode=require' \
  | gcloud secrets create DATABASE_URL --data-file=- --project "$PROJECT_ID"

gcloud secrets add-iam-policy-binding DATABASE_URL \
  --member="serviceAccount:${RUNTIME_SA}" \
  --role=roles/secretmanager.secretAccessor --project "$PROJECT_ID"
```

(Cloud Run mounts it via `--set-secrets DATABASE_URL=DATABASE_URL:latest`, which
the workflow already does. To rotate, add a new secret *version*.)

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
hosting` locally once). The default site serves at `https://<PROJECT_ID>.web.app`
— that URL is the value for `CORS_ALLOWED_ORIGINS` (below).

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
> setup produced). Listed here for reference; `gh variable list --env staging`
> shows the current values. They are configuration, not secrets — WIF means none
> are credentials.

These are **environment-scoped Variables** (Settings → Environments → *name* →
Variables), one set for `staging` and one for `production`.

| Variable | Example (staging) | Notes |
|----------|-------------------|-------|
| `GCP_PROJECT_ID` | `qlab-staging` | |
| `GCP_REGION` | `us-central1` | Cloud Run + Artifact Registry region |
| `WIF_PROVIDER` | `projects/123…/locations/global/workloadIdentityPools/github/providers/github` | printed by step 5 |
| `DEPLOY_SERVICE_ACCOUNT` | `qlab-deployer@qlab-staging.iam.gserviceaccount.com` | |
| `CLOUD_RUN_RUNTIME_SA` | `qlab-api@qlab-staging.iam.gserviceaccount.com` | service runs as this |
| `ARTIFACT_REGISTRY_REPO` | `qlab` | |
| `CLOUD_RUN_SERVICE` | `api-staging` | `api-prod` for production |
| `CORS_ALLOWED_ORIGINS` | `https://qlab-staging.web.app` | the Hosting origin(s); comma-separate if more than one |
| `FIREBASE_PROJECT_ID` | `qlab-staging` | |

`DATABASE_URL` is **not** here — it lives in Secret Manager (step 4).

### Branch protection

On `main`: require the **CI** checks (`test`, `security`, `lint`) to pass,
squash-merge, auto-delete branches (Phase 0). This makes a green `ci.yml` the
gate for every merge.

---

## What's left for you

The GCP infra and all GitHub config (Environments, prod reviewer, variables) are
done. To get the **first green deploy**, you still need:

1. **Neon** — create the project + `staging`/`production` branches and copy each
   branch's connection string (the "Database setup (Neon)" section above).
2. **Secret Manager `DATABASE_URL`** — in **each** project, store that project's
   Neon branch string and grant the runtime SA access (step 4 above). The backend
   revision stays unhealthy until this resolves to a reachable DB (sequencing note
   at the top). You can paste the connection strings to Claude to run step 4 under
   the same exception, or run it yourself.
3. **Firebase Hosting init** — in each Firebase project, enable Hosting (console →
   Hosting → Get started, or `firebase init hosting` once) so the default
   `https://<project>.web.app` site exists for the deploy to target.
4. **Merge this PR**, then approve the production deploy when prompted.

Then verify (below) and paste the URLs back.

---

## Cloud Run health probes (recommended hardening)

The service is built to listen *before* dependencies initialize, so map the
probes accordingly (see `docs/runbook.md` → Health checks):

- **Startup probe → `/readyz`** — holds traffic until the DB connects; give it a
  timeout generous enough for a Neon cold start.
- **Liveness probe → `/healthz`** — restarts only a genuinely wedged process.

Cloud Run's default startup probe is a TCP check on the port, which our
listen-early server passes immediately; that's acceptable for Phase 3. To apply
the HTTP probes explicitly, set them in the console or via a service YAML
(`gcloud run services replace`):

```yaml
# api.run.yaml (excerpt) — startup gated on /readyz, liveness on /healthz
spec:
  template:
    spec:
      containers:
        - image: REGION-docker.pkg.dev/PROJECT/qlab/api:TAG
          startupProbe:
            httpGet: { path: /readyz }
            failureThreshold: 30
            periodSeconds: 2
          livenessProbe:
            httpGet: { path: /healthz }
            periodSeconds: 10
```

---

## Verify (Phase 3 exit criteria)

After a merge deploys staging (and after you approve prod):

```sh
# API up (liveness), and ready (DB reachable):
curl https://<cloud-run-url>/healthz   # {"status":"ok"}
curl https://<cloud-run-url>/readyz    # {"status":"ok"} once the DB connects

# PWA served, and reaching the API cross-origin (CORS):
open https://<PROJECT_ID>.web.app      # hello-world; status line turns teal on success
```

A teal status line on the Hosting page = the browser reached the Cloud Run API
cross-origin, i.e. **CORS works** — the topology tradeoff (decision 0001) is
closed. Paste the two URLs back to Claude to confirm the exit criteria.

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
