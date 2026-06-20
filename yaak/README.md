# yaak

The committed **Yaak** workspace — our living, runnable catalogue of API behaviors.
The rule: every time we introduce a new behavior variation worth reproducing, add a
Yaak request for it (each reschedule scenario, each auth state, each error case).

> **Status:** workspace export lands once the API exists (Phases 5/7). See `docs/PLAN.md`.

## Contents (planned)

    qlab.yaak.json   exported workspace (versioned, shared)

## Conventions

- Commit the exported workspace so the request collection is versioned and shared.
- Use Yaak environments for `local` / `staging` — **never** store prod data-access
  credentials in the committed workspace.
- These requests double as living docs and a manual-regression checklist before pushing.

Yaak: https://yaak.app/ · convention detail in `docs/PLAN.md` ("Yaak as the API
client of record").
