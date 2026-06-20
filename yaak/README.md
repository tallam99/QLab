# yaak

The committed **Yaak** workspace — our living, runnable catalogue of API behaviors.
The rule: every time we introduce a new behavior variation worth reproducing, add a
Yaak request for it (each reschedule scenario, each auth state, each error case).

> **Status:** workspace seeded in Phase 1 with the `/healthz` request and
> `local` / `staging` / `production` environments. It grows as the API does
> (Phases 5/7). See `docs/PLAN.md`.

## Contents

    qlab.yaak.json   exported workspace (versioned, shared)

Requests use a `base_url` environment variable (`${[ base_url ]}/healthz`) so the
same request runs against any environment. `staging` / `production` `base_url`s
are placeholders until the Cloud Run URLs exist (Phase 3).

## Environments & secrets

Environments are colour-coded as a constant visual cue (`local` teal, `staging`
amber, `production` red — the branding palette). Each declares a `base_url` and an
empty `auth_token`.

**Secrets never live in the committed file.** `auth_token` (and any future key) is
present only as an empty placeholder; reference it in requests as
`Authorization: Bearer ${[ auth_token ]}` and put the *real* token in your **local**
Yaak environment, which is not committed. A pre-commit check
(`scripts/check-yaak-secrets.py`, wired via `lefthook.yml`) fails the commit if the
workspace contains a literal token/key/JWT or a non-empty secret-named field — so a
filled secret can't be exported and committed by accident. The same check runs in
CI (Phase 3). Test the checker itself with
`python3 scripts/test_check_yaak_secrets.py`.

> **Convention — name secrets so the guard catches them.** The check only knows a
> variable/header holds a secret if its *name* matches the `SECRET_NAME` regex in
> `scripts/check-yaak-secrets.py` (e.g. contains `token`, `secret`, `key`,
> `password`, `auth`, `credential`). When you introduce a new secret, name it to
> match — otherwise the guard won't require it to stay empty. If a secret genuinely
> can't be named that way, extend the regex in the same change.

## Production guardrails

You can drive `production`, but it's deliberately built to resist thoughtless
mutations:

- The `production` environment is **red** — if the env selector isn't teal/amber,
  stop and think.
- Destructive / state-mutating requests live in the **"Danger — mutates data"
  folder**, never mixed with reads.
- The Danger folder carries an `X-Yaak-Confirm` header set to
  `${[ prompt.text(...) ]}`, which **every request in the folder inherits**. On each
  send, Yaak pops a confirmation dialog; **cancelling aborts the send**. This is
  entirely client-side (a Yaak template function) — no server cooperation, and the
  header is ignored by the API. To add a destructive request, just drop it in the
  folder; it inherits the prompt automatically.

> The prompt fires for danger-folder requests in **every** environment, not just
> production — destructive calls always warrant a confirm. If you're iterating
> locally and the dialog is in the way, temporarily disable the folder header.

## Two ways to use it: GUI (Windows) vs CLI (WSL)

This repo is developed on Windows + WSL2, and Yaak's data lives in a **local
database, not in the committed file**. The GUI and the CLI keep *separate*
databases on their respective OSes:

| | Where it runs | Its database |
|---|---|---|
| **Yaak GUI** | Windows (GUI apps stay on Windows) | Windows `%APPDATA%` |
| **Yaak CLI** (`@yaakapp/cli`) | WSL (Claude uses this) | `~/.local/share/app.yaak.desktop` |

They don't share state. The committed **`qlab.yaak.json` is the bridge** between
them (and the versioned source of truth).

### Using the GUI on Windows (recommended for you)

You don't point the GUI at WSL files — you **import the committed JSON** through
Yaak's settings menu (this is *not* the "Open Folder" option; see the warning
below).

1. Copy the file out of WSL to somewhere the Windows file picker reads cleanly
   (the `\\wsl.localhost\…` share can be flaky in native dialogs). From a WSL
   shell:

       cp ~/repos/qlab/yaak/qlab.yaak.json /mnt/c/Users/<you>/Downloads/

2. In Yaak, open the **settings dropdown** (the gear/`⋯` icon, top of the
   window) → under **"Share Workspace(s)"** click **"Import Data"** → select the
   `qlab.yaak.json` you just copied. This creates the QLab workspace in the GUI's
   own (Windows) database.
3. Pick the `local` / `staging` / `production` environment (top bar) and send
   requests.
4. To commit changes you make in the GUI: same settings dropdown → **"Export
   Data"**, save the file, copy it back over `~/repos/qlab/yaak/qlab.yaak.json`
   (e.g. `cp /mnt/c/Users/<you>/Downloads/qlab.yaak.json ~/repos/qlab/yaak/`),
   then commit (the secret check runs on commit).

> **Don't use "New Workspace → Open Folder".** That's Yaak's *Directory Sync*,
> a different feature that turns a folder into a live-synced workspace. Pointed at
> the WSL repo over the `\\wsl.localhost\…` 9P share it doesn't work reliably, and
> it isn't how this workspace is stored (we commit a single JSON, not a sync
> directory). Use **Import Data / Export Data** instead.

> Re-importing later may create a *second* QLab workspace rather than updating the
> first — so do a one-time Import to get started, then keep the GUI copy and use
> Export to push changes back. If you must pull someone else's changes, re-import
> and delete the stale duplicate.

### Using the CLI in WSL (Claude's path; also available to you)

The [Yaak CLI](https://yaak.app/docs/getting-started/cli-usage)
(`npm i -g @yaakapp/cli`) works on the WSL database. Find ids, then send:

    yaak workspace list                       # workspace ids
    yaak request list <workspace-id>          # request ids
    yaak environment list <workspace-id>      # environment ids (local/staging/production)
    yaak send <request-id> -e <local-env-id>  # fire it against a running server

Note: the CLI has **no import command** — it can't load `qlab.yaak.json`
directly. Claude authors requests via `yaak request create …` and regenerates
the committed export from the CLI's `show` output (the file is the Yaak official
import format: `yaakSchema` + `resources`). So: **GUI ↔ file** via import/export;
**CLI ↔ file** via Claude.

## Conventions

- Commit the exported workspace so the request collection is versioned and shared.
- **Never** store credentials (any environment) in the committed workspace — keep
  them in your local Yaak env and reference them as `${[ auth_token ]}`. The
  pre-commit check enforces this.
- New mutating requests go in the **Danger** folder (they inherit its confirmation
  prompt); reads can live at the top level.
- These requests double as living docs and a manual-regression checklist before pushing.

Yaak: https://yaak.app/ · convention detail in `docs/PLAN.md` ("Yaak as the API
client of record").
