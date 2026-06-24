# frontend — orientation for Claude

The React + TypeScript PWA (Vite), served as a static bundle from Firebase Hosting
and talking to the Cloud Run Connect API cross-origin. Read the root `CLAUDE.md`
and `docs/PLAN.md` (Phases 9–10) first for the phase boundary and the local-vs-cloud
rule. **Current status: Phase 9 — the dev switcher.** Sign in once as the operator
(Google), provision/load a demo workspace, then act as any user in it without
re-pasting tokens, and list/add/cancel slots. **Read `frontend/ARCHITECTURE.md`** for
the full picture (two identities, two transports); it is the source of truth.

## Key files

- `src/main.tsx` — entry. Mounts the provider stack: `TransportProvider` (the **api**
  transport) → `QueryClientProvider` → `SessionProvider` (operator identity) →
  `WorkspaceProvider` (workspace + acting-as) → `App`.
- `src/env.ts` — the **only** place `import.meta.env` is read; validates required
  `VITE_*` vars at startup. Add new config here, never scatter `import.meta.env`.
- `src/firebase.ts` — Firebase app + Auth client; redirects to the Auth emulator
  when `VITE_FIREBASE_AUTH_EMULATOR_HOST` is set (local only).
- `src/api/` — the Connect client edge. **Two transports** (one per identity):
  - `transport.ts` — the **api** transport (→ `qlab.v1`); its interceptor attaches the
    *acting-as* user's `Authorization` + `X-QLab-Lab` from the auth holder.
  - `operatorTransport.ts` — the **operator** transport (→ `qlab.dev.v1`); its
    interceptor attaches the *operator's* own Google token (`auth.currentUser`).
  - `operatorClient.ts` — the imperative generated client over `operatorTransport`.
  - `authHolder.ts` — a tiny mutable holder the api interceptor reads, written by
    `WorkspaceProvider`, so the transport need not be rebuilt when the acting-as
    token/lab change.
  - `headers.ts` — request-header name consts mirroring `internal/api/auth.go`.
- `src/session/SessionProvider.tsx` — owns the **operator** identity (the Firebase/
  Google user). `useSession()` → `{ user, initializing, signInWithGoogle, signOut }`.
- `src/workspace/` — the dev-switcher state.
  - `WorkspaceProvider.tsx` — the loaded workspace, who we act as, the selected pool,
    and the per-user minted-token cache; drives the operator surface and feeds the
    auth holder. `useWorkspace()` is the hook; `canQuery` gates the api calls.
  - `model.ts` — proto → local `{Member, Pool, Workspace}` converters.
- `src/components/` — `SignIn` (operator), `WorkspacePicker` + `ProvisionModal`
  (provision/load a workspace), `ActAsSwitcher` (act-as + pool), `SlotList`
  (list/add/cancel). `*.test.tsx` sit beside their unit.
- `src/protogen/` — **generated** TS from `proto/` (`mage genProto`); committed,
  never hand-edited. Includes `buf/validate/` (vendored for TS via
  `--include-imports`; the Go side resolves it to the BSR module instead — see
  `magefile.go` GenProto).

## Conventions

- **API access uses the generated Connect client only — never hand-written `fetch`.**
  The **public API** uses Connect-Query hooks (`useQuery`/`useMutation` against
  `QlabService.method.*`). The **operator surface** uses the imperative
  `operatorClient` (its flows are imperative: provision-on-submit, mint-on-switch) —
  still the generated client, just not the hooks. The contract of record is `proto/`;
  regenerate with `mage genProto`.
- **Auth headers are attached centrally** — the acting-as credential in
  `api/transport.ts`, the operator credential in `api/operatorTransport.ts`.
  Components never set headers.
- **Tailwind v4** via `@tailwindcss/vite` (no `tailwind.config`); `src/index.css`
  is just `@import "tailwindcss";`. The branding theme is mapped in Phase 10.
- **Biome** is lint + format (`npm run lint` / `lint:fix`); generated `protogen`
  is excluded.
- **Tests**: Vitest + React Testing Library, jsdom env. Drive components off a
  fixed session (mock `useSession`) and an in-memory transport
  (`createRouterTransport`) so tests need no network/Firebase. One test file per
  component, beside it.
- Run `npm run typecheck`, `npm run test`, `npm run lint`, and `npm run build`
  before committing frontend changes.

## Verify

    cd frontend && npm install
    npm run dev          # http://localhost:5173 (needs `mage startStack`)

See `frontend/README.md` for the sign-in / minted-token flow and `docs/runbook.md`
→ "Frontend dev loop".

## Frontend Architecture Rules

The file `frontend/ARCHITECTURE.md` is the source of truth for frontend structure.
Before implementing any frontend feature:
- Check whether the required state, components, or API calls already exist
- Fit new work into the existing architecture; do not create parallel patterns
- If the feature genuinely requires an architectural change, update ARCHITECTURE.md first and flag the change in your response

## Complexity Constraints

- No component file should exceed 300 lines. If a component is growing beyond this, decompose it before continuing.
- State should live as close to where it's used as possible. Do not lift state or introduce context unless two or more components need it.
- Do not introduce a new dependency to solve a problem that can be solved in under ~20 lines of vanilla React.
- If a single subtask or task requires touching more than 5 files, stop and ask whether there's a simpler approach before proceeding. (Exception: a deliberately large epic done in one pass may span more than 5 files in aggregate — the limit is about keeping each unit of change small, not capping a planned multi-part effort. The check still applies to each constituent subtask.)

## Periodic Cleanup

After every 3 completed features, before starting the next one, perform a consolidation pass:
- Identify any duplication introduced since the last pass
- Identify any components or state that have grown beyond their original responsibility
- Refactor without changing behavior, then summarize what changed and why.
