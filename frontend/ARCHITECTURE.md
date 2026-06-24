# Frontend Architecture

This document describes the frontend **as it currently exists**. It is the source of
truth for frontend structure and constrains future work — see `frontend/CLAUDE.md` →
"Frontend Architecture Rules". Update it (and say so) when a change alters anything
below.

The app is the **dev switcher**: sign in once as the **operator** (Google), provision
or load a demo workspace, then act as **any** user in it — fluidly, without re-pasting
tokens — and exercise the queue (list / add / cancel slots). There is no router and no
global store library. Two ideas organize everything:

- **Two identities.** The *operator* (the Google-signed-in user) drives the
  staging/local-only operator surface (`qlab.dev.v1.DevService`). The *acting-as* user
  (a seeded user we impersonate via a minted token) drives the public API
  (`qlab.v1.QlabService`).
- **Two transports, one per identity.** The **api transport** carries the acting-as
  user's token; the **operator transport** carries the operator's own Google token.

## Module map

```
src/
  main.tsx                      entry; mounts the provider stack
  App.tsx                       layout shell + top-level gating
  env.ts                        the only reader of import.meta.env
  firebase.ts                   Firebase app + Auth client singletons
  index.css                     `@import "tailwindcss";` only
  api/
    transport.ts                api transport (→ qlab.v1) + acting-as auth interceptor
    operatorTransport.ts        operator transport (→ qlab.dev.v1) + operator-token interceptor
    operatorClient.ts           imperative generated client over operatorTransport
    authHolder.ts               mutable singleton bridging React → the api interceptor
    headers.ts                  request-header name constants
  session/
    SessionProvider.tsx         context: the OPERATOR identity (Firebase user)
  workspace/
    WorkspaceProvider.tsx       context: workspace + acting-as + token cache; feeds authHolder
    model.ts                    proto → local {Member, Pool, Workspace} converters
  components/
    SignIn.tsx                  operator Google sign-in / sign-out control
    WorkspacePicker.tsx         provision a new / load an existing workspace
    ProvisionModal.tsx          new-workspace form (feature, member/resource counts)
    ActAsSwitcher.tsx           pick which user to act as + which pool
    SlotList.tsx                list / add / cancel slots for the acting-as user
    ComingSoon.tsx              inert placeholder shown when the switcher is disabled (prod)
    *.test.tsx                  one test file beside each non-trivial unit
  protogen/                     GENERATED Connect/protobuf TS (never hand-edited)
```

### Provider stack (`main.tsx`)

Mounted outermost → innermost; this order is load-bearing:

```
TransportProvider (transport = the api transport)   Connect-Query default transport (qlab.v1)
  QueryClientProvider (queryClient)                 TanStack Query cache
    SessionProvider                                 operator identity
      WorkspaceProvider                             workspace + acting-as; feeds authHolder
        App
```

`queryClient` is a default `new QueryClient()` (no custom `staleTime`/`gcTime`/retry).
The `TransportProvider` transport is the **api** transport, so every Connect-Query
hook (`SlotList`) targets `qlab.v1` as the acting-as user. Operator calls do **not**
go through this provider — they use `operatorClient` (its own transport) imperatively.

## 1. Component inventory

Props, rendered output, and **owned** state (`useState`/`useRef` in that component)
as implemented. Styling is inline Tailwind; there is no shared component library yet.

### `App` (`App.tsx`)
- **Props/owned state:** none.
- **Reads:** `useSession()` → `{ user, initializing }`; `useWorkspace()` → `{ workspace, error }`.
- **Prod gate:** if `env.devSwitcherEnabled` is false (the default — production), renders
  `<ComingSoon />` and nothing else. The switcher below only renders in local/staging
  builds (`VITE_DEV_SWITCHER=true`), where the operator surface it needs is mounted.
- **Renders (switcher enabled):** header (`<h1>QLab</h1>` + `<SignIn />`); then
  `"Starting…"` while `initializing`, a sign-in prompt when there is no operator
  `user`, else the operator `error` (if any) + `<WorkspacePicker />` and — once a
  `workspace` is loaded — `<ActAsSwitcher />` + `<SlotList />`.

### `SignIn` (`components/SignIn.tsx`)
- **Owned state:** `error: string | null`.
- **Reads:** `useSession()` → `{ user, signInWithGoogle, signOut }`.
- **Renders:** signed in — the operator `email ?? uid` + "Sign out"; signed out — a
  "Sign in with Google" button (errors captured into `error`).

### `WorkspacePicker` (`components/WorkspacePicker.tsx`)
- **Owned state:** `labs: LabSummary[]`, `selectedLabId: string`, `showModal: boolean`,
  `listError: string | null`.
- **Reads:** `useWorkspace()` → `{ workspace, loadWorkspace, listWorkspaces, busy }`.
- **Behaviour:** on mount calls `listWorkspaces()` (operator `ListLabs`) to fill the
  dropdown; "Load" calls `loadWorkspace(selectedLabId)`; "New workspace" opens
  `<ProvisionModal>`, refreshing the list on close. Shows the loaded workspace name.

### `ProvisionModal` (`components/ProvisionModal.tsx`)
- **Props:** `{ onClose: () => void }`.
- **Owned state:** `feature: string` (default `"demo"`), `memberCount: number` (3),
  `resourceCount: number` (2).
- **Reads:** `useWorkspace()` → `{ provision, busy }`.
- **On submit:** `await provision(feature, memberCount, resourceCount)` then `onClose()`.

### `ActAsSwitcher` (`components/ActAsSwitcher.tsx`)
- **Props/owned state:** none.
- **Reads:** `useWorkspace()` → `{ workspace, actingUserId, actAs, poolId, selectPool, actingMember, busy }`.
- **Renders:** nothing until a `workspace` loads; then an "Act as" `<select>` of
  members (→ `actAs(userId)`) and a "Pool" `<select>` (→ `selectPool`), plus the
  current acting member. `memberLabel` formats `name||email (role)`.

### `SlotList` (`components/SlotList.tsx`)
- **Props/owned state:** none (server state in TanStack Query; mutation state in the
  Connect-Query mutations).
- **Reads:** `useWorkspace()` → `{ poolId, canQuery }`.
- **Data:** `useQuery(QlabService.method.listSlots, { resourcePoolId: poolId ?? "" }, { enabled: canQuery })`,
  plus `useMutation(QlabService.method.createSlot)` and `…cancelSlot`. After a mutation
  it `await refetch()`s the list.
- **Renders:** an "Add 30-min slot" button (creates a now-start, 30-min, 0-lookahead
  slot); loading / error (`<code-name>: <rawMessage>` for a `ConnectError`) / empty
  states; else a table (`#`, `Status` with `status <n>` fallback, `Start` via
  `formatStart`, `Duration`, and a per-row **Cancel** shown only for `SCHEDULED`/`ACTIVE`).

## 2. State map

Two React contexts; everything else is component-local, the TanStack Query cache, or
the auth-holder singleton.

| State | Type | Lives in | Why there |
|---|---|---|---|
| `user` (operator) | `User \| null` | `SessionProvider` (`useState`) | The Google-signed-in operator; authenticates the **operator** transport. Read by `SignIn`/`App`. |
| `initializing` | `boolean` | `SessionProvider` (`useState`) | True until the first `onAuthStateChanged`, to avoid flashing the signed-out view. |
| `workspace` | `Workspace \| null` | `WorkspaceProvider` (`useState`) | The loaded demo lab + roster + pools. Read by `App`/`WorkspacePicker`/`ActAsSwitcher`. |
| `actingUserId` | `string \| null` | `WorkspaceProvider` (`useState`) | Which member we act as; selects the cached token fed to the api transport. |
| `poolId` | `string \| null` | `WorkspaceProvider` (`useState`) | The pool `SlotList` queries/mutates. |
| `busy` / `error` | `boolean` / `string \| null` | `WorkspaceProvider` (`useState`) | In-flight + last-error for operator calls (provision/mint/load). |
| `tokenCache` | `Map<userId, token>` | `WorkspaceProvider` (`useRef`) | Minted ID tokens per acting-as user, so switching back never re-mints. A ref: the interceptor reads it lazily, mutating it needn't re-render. |
| `SignIn.error`, `WorkspacePicker.*`, `ProvisionModal.*` | various | the component (`useState`) | Single-consumer UI/form state; not lifted. |
| Server data (`ListSlots`) | `ListSlotsResponse` | TanStack Query cache | Server state, keyed by Connect-Query on method + input. |
| Live api credential + lab | `{ getToken, labId }` | `api/authHolder.ts` singleton | Non-React bridge from `WorkspaceProvider` to the api interceptor (below). |

Derived (not stored): `WorkspaceProvider.canQuery = workspace !== null && actingUserId
!== null && poolId !== null`, and `actingMember` (looked up from `workspace.members`),
both computed in its `useMemo`.

### The auth holder, and why two transports

The **api transport** (`api/transport.ts`, the Connect-Query default) carries the
*acting-as* user's token + lab. It is created once at module load; rather than rebuild
it on every switch, `WorkspaceProvider` writes the current acting-as credential into a
plain mutable module object (`authHolder`) and the interceptor reads it per request.
`WorkspaceProvider` is the **sole writer**, the interceptor the **sole reader**. The
write is **synchronous during render** (not a `useEffect`): a child query fired on the
same switch must not read a stale token/lab (child effects run before a parent's). It
is idempotent.

The **operator transport** (`api/operatorTransport.ts`) carries the *operator's* own
Google token, read directly from `auth.currentUser?.getIdToken()` in its interceptor —
no holder needed, since there is only ever one operator (the signed-in user).

## 3. Data flow

Two paths. **Operator** calls are imperative (driven by user actions); the **acting-as
public-API** read is declarative (Connect-Query).

```
Operator path (provision / mint / list / load):
  WorkspacePicker / ProvisionModal / ActAsSwitcher
    → WorkspaceProvider action  (operatorClient.provisionLab / mintToken / listLabs / getLab)
    → operatorTransport interceptor sets Authorization: Bearer <operator Google token>
    → POST {baseUrl}/qlab.dev.v1.DevService/<Method>
    → WorkspaceProvider updates workspace / actingUserId / tokenCache

Acting-as path (the queue):
  WorkspaceProvider state (workspace + actingUserId)
    → render-phase write → authHolder { getToken: cached acting-as token, labId }
    → SlotList: useQuery(listSlots, {resourcePoolId: poolId}, {enabled: canQuery})
              + useMutation(createSlot / cancelSlot)
    → api transport interceptor reads authHolder → Authorization + X-QLab-Lab
    → POST {baseUrl}/qlab.v1.QlabService/<Method>
    → TanStack Query caches ListSlots; mutations refetch()
    → DOM
```

- **Caching:** TanStack Query with default options; the `ListSlots` key is derived by
  Connect-Query from method + input (`resourcePoolId`). Mutations don't use cache
  invalidation — `SlotList` `await refetch()`s after `createSlot`/`cancelSlot`.
- **Token cache:** `actAs(userId)` mints via the operator client only on a cache miss;
  a hit (a user already acted as) sets `actingUserId` with no network call.
- **Local transforms:** `model.ts` flattens operator proto responses to
  `{Member, Pool, Workspace}`; `SlotList` keeps `formatStart` + the `SlotStatus`
  name lookup. No normalization layer or SSE/streaming path (the `QueueEvent` envelope
  is generated but unused).

## 4. API boundary

The frontend calls **two** Connect services over HTTP POST at
`/<package>.<Service>/<Method>`, each on its own transport. Access is only through the
generated clients — Connect-Query hooks for the public API, the imperative
`operatorClient` for the operator surface; no hand-written `fetch`.

### `qlab.dev.v1.DevService` — operator transport (operator's Google token)

| RPC | Called from | Request | Response (fields used) |
|---|---|---|---|
| `ListLabs` | `WorkspacePicker` (on mount) | `{ feature: "" }` | `{ labs: LabSummary[] }` (lab id/name, user/resource counts) |
| `GetLab` | `WorkspaceProvider.loadWorkspace` | `{ labId }` | `{ lab, members, pools, … }` |
| `ProvisionLab` | `WorkspaceProvider.provision` | `{ feature, memberCount, resourceCount }` | `{ lab, pool, members, … }` |
| `MintToken` | `WorkspaceProvider.actAs` (cache miss) | `{ userId }` | `{ idToken, user }` |

`TeardownLab` is generated/available but not wired. The single header is
`Authorization: Bearer <operator token>`; no `X-QLab-Lab` (the operator surface is
cross-tenant).

### `qlab.v1.QlabService` — api transport (acting-as minted token)

| RPC | Called from | Request | Response |
|---|---|---|---|
| `ListSlots` | `SlotList` (`useQuery`) | `{ resourcePoolId }` | `{ slots: Slot[] }` |
| `CreateSlot` | `SlotList` "Add slot" | `{ resourcePoolId, desiredStart, lookaheadMinutes: 0, durationMinutes: 30, note: "" }` | `{ result? }` |
| `CancelSlot` | `SlotList` per-row | `{ slotId }` | `{ result? }` |

Every api request carries `Authorization: Bearer <acting-as token>` + `X-QLab-Lab:
<workspace lab>`, attached centrally by the api interceptor (components never set
headers). `ClockIn`/`ClockOut`/`PokeOccupant`/`ForceClockOut`/`ForceNoShow` remain
generated-but-unwired. Header names: `api/headers.ts`.

### Base URL & transport

Both transports use `baseUrl = env.apiBaseUrl || window.location.origin`:
- **Local:** `VITE_API_BASE_URL` empty → same-origin → the Vite proxy forwards both
  `"/qlab.v1."` and `"/qlab.dev.v1."` to `http://localhost:8090`.
- **Staging:** `VITE_API_BASE_URL` is the Cloud Run URL → cross-origin (CORS). In a
  production build `env.ts` **requires** it (throws if empty, rather than 404ing
  against the Hosting origin). The operator surface is staging/local-only and absent
  in production, so the switcher is non-functional in a prod build by construction.

## 5. Auth flow

### Operator identity (drives `DevService`)
1. `SignIn` → `signInWithGoogle()` → `signInWithPopup`. Locally the SDK points at the
   Auth emulator (`connectAuthEmulator`), so any email works; staging uses real Google.
2. `SessionProvider` tracks it via `onAuthStateChanged` (`user` + `initializing`).
3. The operator transport's interceptor attaches `auth.currentUser.getIdToken()` to
   every `DevService` call. The backend checks the verified email against the
   staging/local operator allowlist (`OPERATOR_ALLOWED_EMAILS`, decision 0008) — the
   browser never holds the operator secret.

### Acting-as identity (drives `QlabService`)
1. `actAs(userId)` mints an ID token via `operatorClient.mintToken` (or reuses the
   `tokenCache` entry) and sets `actingUserId`.
2. `WorkspaceProvider` writes `{ getToken: () => tokenCache.get(actingUserId), labId:
   workspace.labId }` into `authHolder` (render-phase).
3. The api interceptor reads the holder per request → `Authorization` + `X-QLab-Lab`.

### Storing / clearing
- All tokens are **in-memory only** (no `localStorage`/cookies written by app code).
  `tokenCache` lives for the session; loading/provisioning a different workspace
  invalidates it (its tokens belong to the prior lab's users), and `reset()` clears
  everything.
- **Sign-out / account switch fully resets the session.** `WorkspaceProvider` watches
  the operator's uid (`useSession().user?.uid`) and `reset()`s on any change — clearing
  the workspace, the acting-as selection, **and the cached minted tokens** — so a
  signed-out or different operator can never see or act on the previous session's data
  (a token refresh keeps the same uid, so this doesn't fire spuriously). This is the
  one place `WorkspaceProvider` depends on `SessionProvider`.
- `canQuery` (workspace + acting-as user + pool) gates `SlotList`'s query. No explicit
  query-cache eviction — TanStack Query's default `gcTime` + the `enabled` gate suffice
  once the holder stops yielding a token.

## Constraints captured for future work

- Generated `protogen/` is committed (regenerate with `mage genProto`); never hand-edit.
- **Two contexts only** (`SessionProvider` = operator, `WorkspaceProvider` = workspace/
  acting-as). New shared state must clear the "two or more consumers" bar before adding
  a third context or lifting state.
- **Public-API access uses Connect-Query hooks**; the **operator surface uses the
  imperative `operatorClient`** (its flows are imperative: provision-on-submit,
  mint-on-switch). Both are the generated client — neither is hand-written `fetch`.
- Api auth headers are attached only in `api/transport.ts`; operator auth only in
  `api/operatorTransport.ts`. Components never set headers.
- `env.ts` is the only reader of `import.meta.env`.
