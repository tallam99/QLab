# frontend — orientation for Claude

The React + TypeScript PWA (Vite), served as a static bundle from Firebase Hosting
and talking to the Cloud Run Connect API cross-origin. Read the root `CLAUDE.md`
and `docs/PLAN.md` (Phases 9–10) first for the phase boundary and the local-vs-cloud
rule. **Current status: Phase 9 (scaffold)** — Google sign-in + one authenticated
`ListSlots` call work; the product UI is Phase 10.

## Key files

- `src/main.tsx` — entry. Mounts the provider stack: `TransportProvider`
  (Connect-Query, the cross-origin transport) → `QueryClientProvider` (TanStack
  Query) → `SessionProvider` → `App`.
- `src/env.ts` — the **only** place `import.meta.env` is read; validates required
  `VITE_*` vars at startup. Add new config here, never scatter `import.meta.env`.
- `src/firebase.ts` — Firebase app + Auth client; redirects to the Auth emulator
  when `VITE_FIREBASE_AUTH_EMULATOR_HOST` is set (local only).
- `src/api/` — the Connect client edge:
  - `transport.ts` — one long-lived `createConnectTransport` with an interceptor
    that attaches `Authorization: Bearer <token>` and `X-QLab-Lab` to every call.
  - `authHolder.ts` — a tiny mutable holder the interceptor reads, written by the
    session provider, so the transport need not be rebuilt when the token/lab
    change.
  - `headers.ts` — request-header name consts mirroring the backend
    (`internal/api/auth.go`).
- `src/session/SessionProvider.tsx` — owns auth state: the Firebase user, an
  optional pasted **operator-minted token** (the staging act-as path, decision
  0008), and the selected lab + pool. Mirrors the credential + lab into the auth
  holder. `useSession()` is the hook; `canQuery` gates API calls.
- `src/components/` — `SignIn` (Google), `DevTokenPanel` (paste minted token +
  lab/pool), `SlotList` (the one real `ListSlots` call). `*.test.tsx` sit beside
  their component.
- `src/protogen/` — **generated** TS from `proto/` (`mage genProto`); committed,
  never hand-edited. Includes `buf/validate/` (vendored for TS via
  `--include-imports`; the Go side resolves it to the BSR module instead — see
  `magefile.go` GenProto).

## Conventions

- **API access uses the generated Connect client only** — `useQuery` from
  `@connectrpc/connect-query` against `QlabService.method.*`, never hand-written
  `fetch`. The contract of record is `proto/`; regenerate with `mage genProto`.
- **Auth headers are attached centrally** in `api/transport.ts`; components never
  set them. Both sign-in modes (Google / minted token) flow through the session
  provider → auth holder → interceptor.
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
