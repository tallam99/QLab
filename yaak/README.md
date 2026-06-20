# yaak

The committed **Yaak** workspace — our living, runnable catalogue of API behaviors.
The rule: every time we introduce a new behavior variation worth reproducing, add a
Yaak request for it (each reschedule scenario, each auth state, each error case).

> **Status:** workspace seeded in Phase 1 with the `/healthz` request and
> `local` / `staging` environments. It grows as the API does (Phases 5/7). See
> `docs/PLAN.md`.

## Contents

    qlab.yaak.json   exported workspace (versioned, shared)

Requests use a `base_url` environment variable (`${[ base_url ]}/healthz`) so the
same request runs against `local` and `staging`. The `staging` `base_url` is a
placeholder until the Cloud Run URL exists (Phase 3).

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

WSL does **not** break the GUI, because you never point the GUI at WSL files —
you import the committed export instead:

1. In Yaak: **Settings → Data → Import**, choose
   `\\wsl.localhost\Ubuntu\home\<user>\repos\qlab\yaak\qlab.yaak.json`
   (or copy it to Windows first). This recreates the workspace in the GUI's
   Windows database.
2. Pick the `local` or `staging` environment (top bar) and send requests.
3. If you add/change requests in the GUI and want them committed, **export** the
   workspace back over `qlab.yaak.json` and commit it.

> Avoid Yaak's *Directory Sync* pointed at the `\\wsl.localhost\…` path — live
> file-watching across the WSL 9P share is unreliable. Import/export is robust.

### Using the CLI in WSL (Claude's path; also available to you)

The [Yaak CLI](https://yaak.app/docs/getting-started/cli-usage)
(`npm i -g @yaakapp/cli`) works on the WSL database. Find ids, then send:

    yaak workspace list                       # workspace ids
    yaak request list <workspace-id>          # request ids
    yaak environment list <workspace-id>      # environment ids (local/staging)
    yaak send <request-id> -e <local-env-id>  # fire it against a running server

Note: the CLI has **no import command** — it can't load `qlab.yaak.json`
directly. Claude authors requests via `yaak request create …` and regenerates
the committed export from the CLI's `show` output (the file is the Yaak official
import format: `yaakSchema` + `resources`). So: **GUI ↔ file** via import/export;
**CLI ↔ file** via Claude.

## Conventions

- Commit the exported workspace so the request collection is versioned and shared.
- Use Yaak environments for `local` / `staging` — **never** store prod data-access
  credentials in the committed workspace.
- These requests double as living docs and a manual-regression checklist before pushing.

Yaak: https://yaak.app/ · convention detail in `docs/PLAN.md` ("Yaak as the API
client of record").
