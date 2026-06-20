# yaak

The committed **Yaak** workspace — our living, runnable catalogue of API behaviors.
The rule: every time we introduce a new behavior variation worth reproducing, add a
Yaak request for it (each reschedule scenario, each auth state, each error case).

> **Status:** workspace seeded in Phase 1 with the `/healthz` request and
> `local` / `staging` environments. It grows as the API does (Phases 5/7). See
> `docs/PLAN.md`.

## Contents

    qlab.yaak.json   exported workspace (versioned, shared)

## Working with the workspace (CLI)

The [Yaak CLI](https://yaak.app/docs/getting-started/cli-usage)
(`npm i -g @yaakapp/cli`) operates on the local Yaak SQLite database, so requests
are authored as resources and the workspace is committed as a single JSON
export. To send the `/healthz` request against a locally-running server:

    yaak send <request-id> -e <local-env-id>

Requests use a `base_url` environment variable (`${[ base_url ]}/healthz`) so the
same request runs against `local` and `staging`. The `staging` `base_url` is a
placeholder until the Cloud Run URL exists (Phase 3). The committed
`qlab.yaak.json` is the Yaak official import format (`yaakSchema` + `resources`);
re-import it via the Yaak app to recreate the workspace.

## Conventions

- Commit the exported workspace so the request collection is versioned and shared.
- Use Yaak environments for `local` / `staging` — **never** store prod data-access
  credentials in the committed workspace.
- These requests double as living docs and a manual-regression checklist before pushing.

Yaak: https://yaak.app/ · convention detail in `docs/PLAN.md` ("Yaak as the API
client of record").
