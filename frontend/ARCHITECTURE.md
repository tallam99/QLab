# Frontend Architecture

This document describes the frontend **as it currently exists** (Phase 9 scaffold).
It is the source of truth for frontend structure and is used to constrain future
work — see `frontend/CLAUDE.md` → "Frontend Architecture Rules". Update it (and say
so) when a change alters anything below.

The app is a single screen: authenticate (Google sign-in or a pasted operator-minted
token), select a lab + resource pool, and render one real authenticated call
(`ListSlots`). There is no router, no global store library, and one React context.

## Module map

```
src/
  main.tsx                      entry; mounts the provider stack
  App.tsx                       the single screen / layout shell
  env.ts                        the only reader of import.meta.env
  firebase.ts                   Firebase app + Auth client singletons
  index.css                     `@import "tailwindcss";` only
  setupTests.ts                 Vitest/RTL setup
  vite-env.d.ts                 Vite type shims
  api/
    transport.ts                one Connect transport + auth interceptor
    authHolder.ts               mutable singleton bridging React state → interceptor
    headers.ts                  request-header name constants
  session/
    SessionProvider.tsx         the one context; owns all auth + selection state
  components/
    SignIn.tsx                  Google sign-in / sign-out control
    DevTokenPanel.tsx           paste minted token + lab/pool ids
    SlotList.tsx                renders ListSlots
    *.test.tsx                  one test file beside each non-trivial component
  protogen/                     GENERATED Connect/protobuf TS (never hand-edited)
```

### Provider stack (`main.tsx`)

Mounted outermost → innermost; this order is load-bearing:

```
TransportProvider (transport)          Connect-Query: supplies the transport
  QueryClientProvider (queryClient)    TanStack Query: the cache
    SessionProvider                    auth + selection context
      App
```

`queryClient` is a default `new QueryClient()` — no custom `staleTime`, `gcTime`,
retry, or refetch options are set anywhere. `transport` is a module-level singleton
imported from `api/transport.ts` (not created in `main.tsx`).

## 1. Component inventory

Five components exist. Props, rendered output, and **owned** state (i.e. `useState`
in that component) are listed exactly as implemented.

### `App` (`App.tsx`)
- **Props:** none.
- **Owned state:** none.
- **Reads:** `useSession()` → `{ canQuery, selection, initializing }`.
- **Renders:** the page chrome — a header (`<h1>QLab</h1>` + `<SignIn />`) and a
  `<main>` containing `<DevTokenPanel />` and a "Slots" section. The section shows
  `pool <first 8 chars>…` when a `selection` exists, then conditionally renders:
  `"Starting…"` while `initializing`, `<SlotList />` when `canQuery`, else a prompt
  to sign in / select a lab + pool.

### `SignIn` (`components/SignIn.tsx`)
- **Props:** none.
- **Owned state:** `error: string | null` (last sign-in error, shown in red).
- **Reads:** `useSession()` → `{ user, signInWithGoogle, signOut }`.
- **Renders:** when `user` is set — the user's `email ?? uid` and a "Sign out"
  button (`onClick → void signOut()`). When signed out — a "Sign in with Google"
  button (`onClick` clears `error`, calls `signInWithGoogle()`, and `.catch`es into
  `error`) plus the error text if present.

### `DevTokenPanel` (`components/DevTokenPanel.tsx`)
- **Props:** none.
- **Owned state:** `labId: string`, `poolId: string`, `token: string` — controlled
  form inputs. `labId`/`poolId` initialize from the current `selection` (`?? ""`);
  `token` always initializes `""` (a minted token is never read back out of session).
- **Reads:** `useSession()` → `{ setSelection, setManualToken, selection,
  manualToken, clear, user, canQuery }`.
- **Renders:** a `<form>` with Lab ID, Pool ID, and "Minted token (optional)" text
  inputs; an "Active credential" line when `canQuery` (`minted token` vs
  `Google sign-in`, noting when a token overrides a live Google session); an "Apply"
  submit button (disabled until both lab and pool are non-blank); and a "Clear"
  button shown only when a `selection` or `manualToken` is active.
- **On submit:** `setManualToken(token` trimmed, or `null` if blank`)` then
  `setSelection({ labId: trimmed, poolId: trimmed })`. **On Clear:** `clear()`.

### `SlotList` (`components/SlotList.tsx`)
- **Props:** none.
- **Owned state:** none (server state lives in TanStack Query).
- **Reads:** `useSession()` → `{ selection, canQuery }`.
- **Data:** `useQuery(QlabService.method.listSlots, { resourcePoolId: poolId },
  { enabled: canQuery })` where `poolId = selection?.poolId ?? ""`.
- **Renders:** `"Loading slots…"` while `isLoading`; a red error line on `error`
  (`<code-name>: <rawMessage>` for a `ConnectError` — `Code[error.code]` maps the
  numeric code to its name — else `String(error)`); `"No slots in this pool yet."`
  when the response has zero slots; otherwise a table with columns `# (slotPriority)`,
  `Status` (`SlotStatus[slot.status]`, the enum's string name, falling back to
  `status <n>` for an unknown value), `Start` (`formatStart`), `Duration`
  (`durationMinutes + "m"`), keyed by `slot.id`.
- **Local transform:** `formatStart(actual?, desired?)` picks `actualStart ?? desiredStart`
  and renders `timestampDate(ts).toLocaleString()`, or `"—"` when neither is set.

There is no shared/presentational component library yet (no `Button`, `Input`, etc.);
styling is inline Tailwind utility classes per element.

## 2. State map

All application state lives in exactly one of three places. There is **one** React
context (`SessionProvider`); everything else is component-local or server-cache.

| State | Type | Lives in | Why there |
|---|---|---|---|
| `user` | `User \| null` | `SessionProvider` (`useState`) | Firebase auth identity; read by `SignIn`, gates `canQuery`, and feeds the token getter. Shared across components → context. |
| `manualToken` | `string \| null` | `SessionProvider` (`useState`) | Operator-minted act-as token (decision 0008); takes precedence over the Firebase token. Shared (panel writes, interceptor reads) → context. |
| `selection` | `LabSelection \| null` (`{ labId, poolId }`) | `SessionProvider` (`useState`) | The lab + pool the caller acts in. Written by `DevTokenPanel`, read by `App`/`SlotList`, and `labId` is mirrored to the auth holder. Shared → context. |
| `initializing` | `boolean` | `SessionProvider` (`useState`) | True until the first `onAuthStateChanged` fires, so the UI shows "Starting…" instead of flashing the signed-out state. Shared → context. |
| `SignIn.error` | `string \| null` | `SignIn` (`useState`) | Only `SignIn` renders the last sign-in error. Not lifted — single consumer. |
| `DevTokenPanel.labId/poolId/token` | `string` | `DevTokenPanel` (`useState`) | Draft form input before "Apply". Local to the form; committed to `selection`/`manualToken` only on submit. Not lifted. |
| Server data (`ListSlots` response) | `ListSlotsResponse` | TanStack Query cache | Server state, not UI state — cached/refetched by Connect-Query keyed on method + input. Never copied into `useState`. |
| Live auth credential + lab | `{ getToken, labId }` | `api/authHolder.ts` module singleton | A non-React bridge so the long-lived transport interceptor reads fresh values without React re-rendering it or rebuilding the transport. Written **from** React, read **outside** React. |

`canQuery` is **derived**, not stored: `(manualToken !== null || user !== null) &&
selection !== null`. It is recomputed in the `SessionProvider` `useMemo`.

### Why the auth holder exists (the one non-obvious piece)

The transport is created **once** at module load (`api/transport.ts`), but the token
and selected lab change as the user signs in / switches. Rather than rebuild the
transport on every change, `SessionProvider` writes the current credential + lab into
a plain mutable module object (`authHolder`), and the interceptor reads it
per-request. This is the deliberate seam between React state and the non-React
transport — `SessionProvider` is the **sole writer**, the interceptor the **sole
reader**.

The write happens **synchronously during `SessionProvider`'s render**, not in a
`useEffect`. This is intentional: `SlotList`'s query fires from a child effect, and
child effects run before a parent's effects would — so an effect-based write here
would let the first fetch after a lab/credential switch attach the *previous* lab or
token. The render-phase write is current before any child commits; it is idempotent
(same values → same holder), so re-running it each render is harmless.

## 3. Data flow

One read path exists end-to-end:

```
SessionProvider state (user/manualToken/selection)
  │  render-phase write mirrors → authHolder { getToken, labId }
  ▼
SlotList: useQuery(listSlots, { resourcePoolId }, { enabled: canQuery })
  │  Connect-Query builds the request; enabled gate prevents firing until canQuery
  ▼
transport interceptor `auth` reads authHolder, sets Authorization + X-QLab-Lab
  ▼
createConnectTransport POSTs to {baseUrl}/qlab.v1.QlabService/ListSlots
  │  local: baseUrl = window.location.origin → Vite proxy → http://localhost:8090
  │  staging/prod: baseUrl = VITE_API_BASE_URL (cross-origin, CORS)
  ▼
TanStack Query caches the ListSlotsResponse (keyed on method + input)
  ▼
SlotList reads { data, error, isLoading }; renders table / states
  │  transform: formatStart(actualStart ?? desiredStart); SlotStatus[status] → name
  ▼
DOM
```

- **Caching:** TanStack Query with default options. The query key is derived by
  Connect-Query from the method descriptor + the input message (`resourcePoolId`), so
  changing the pool issues a distinct cache entry. No manual `queryKey`, `staleTime`,
  invalidation, or `setQueryData` calls exist. No mutations are wired (the 7 mutating
  RPCs are unused — see §4).
- **Local transformation:** only in `SlotList` — `formatStart` and the
  `SlotStatus`-enum-to-name lookup. No normalization layer, selector library, or
  derived-store; the proto message is rendered close to as-is.
- **No real-time path:** the `QueueEvent` SSE envelope exists in the generated types
  but **no SSE/streaming subscription is implemented**. Data is fetch-on-demand only.

## 4. API boundary

The frontend talks to **one** backend service, the Connect-RPC `qlab.v1.QlabService`,
over HTTP POST at `/<package>.<Service>/<Method>`. Access is **only** through the
generated client (`src/protogen`) via Connect-Query — there are no hand-written
`fetch` calls.

**Every** request carries two headers, attached centrally by the `auth` interceptor
in `api/transport.ts` (components never set them):

- `Authorization: Bearer <id-token>` — set only when a token is available.
- `X-QLab-Lab: <lab-uuid>` — set only when a lab is selected.

(Header names are the constants in `api/headers.ts`, mirroring the backend's
`internal/api/auth.go`.)

### Endpoints actually called

| RPC | Called from | Request sent | Response expected |
|---|---|---|---|
| `QlabService.ListSlots` | `SlotList` (`useQuery`) | `{ resourcePoolId: string }` | `{ slots: Slot[] }` (`ListSlotsResponse`) |

`ListSlots` is the **only** RPC the app currently invokes.

### Endpoints available in the generated client but NOT called

These exist in `protogen` and are part of the contract, but no UI wires them yet
(Phase 10). Listed so future work fits the existing client rather than inventing one.
All requests/responses are exactly as generated:

| RPC | Request | Response |
|---|---|---|
| `CreateSlot` | `{ resourcePoolId, desiredStart?, lookaheadMinutes, durationMinutes, note }` | `{ result?: RescheduleResult }` |
| `ClockIn` | `{ slotId }` | `{ result?: RescheduleResult }` |
| `ClockOut` | `{ slotId }` | `{ result?: RescheduleResult }` |
| `CancelSlot` | `{ slotId }` | `{ result?: RescheduleResult }` |
| `PokeOccupant` | `{ slotId }` | `{}` (empty) |
| `ForceClockOut` | `{ slotId }` | `{ result?: RescheduleResult }` |
| `ForceNoShow` | `{ slotId }` | `{ result?: RescheduleResult }` |

The `qlab.dev.v1.DevService` operator surface (`ProvisionLab`, `MintToken`, etc.) is
generated into `protogen/qlab/dev/v1` **but the frontend does not call it** — tokens
and ids are obtained out-of-band (curl/operator) and pasted into `DevTokenPanel`.

### `Slot` shape (fields the UI reads)

`SlotList` reads: `id`, `slotPriority` (int), `status` (`SlotStatus` enum),
`actualStart?`/`desiredStart?` (`google.protobuf.Timestamp`), `durationMinutes` (int).
The full `Slot` message (see `protogen/qlab/v1/types_pb.ts`) also carries `labId`,
`userId`, `resourcePoolId`, `assignedResourceId`, `lookaheadMinutes`,
`committedStart?`, `note` — none currently rendered.

### Base URL & transport

`baseUrl = env.apiBaseUrl || window.location.origin`:
- **Local:** `VITE_API_BASE_URL` is empty → same-origin → the Vite dev server proxy
  (`vite.config.ts`) forwards `"/qlab.v1."` and `"/qlab.dev.v1."` paths to
  `http://localhost:8090`. No CORS on localhost.
- **Staging/prod:** `VITE_API_BASE_URL` is the Cloud Run API URL → genuinely
  cross-origin → relies on the API's CORS allow-list (decision 0001). In a production
  build (`import.meta.env.PROD`) `env.ts` **requires** this var to be non-empty and
  throws at startup otherwise — an empty value would otherwise fall back to the
  Hosting origin and 404 every RPC.

## 5. Auth flow

Two credential sources feed one transport. `manualToken` always wins over the
Firebase token.

### Obtaining a token

1. **Google sign-in (production path).** `SignIn` → `signInWithGoogle()` →
   `signInWithPopup(auth, googleProvider)` (`firebase.ts`). Locally the SDK is
   pointed at the Auth emulator (`connectAuthEmulator`, gated on
   `VITE_FIREBASE_AUTH_EMULATOR_HOST`), so any email works without a real Google
   account. `SessionProvider` subscribes once via `onAuthStateChanged`, which sets
   `user` and flips `initializing` to false.
2. **Pasted minted token (staging/local act-as, decision 0008).** `DevTokenPanel`
   "Apply" calls `setManualToken(token)`. This is an operator-minted ID token obtained
   out-of-band; it needs no Firebase session.

### Storing

- `user` and `manualToken` live in `SessionProvider` `useState` (in-memory only).
- **No token is persisted** by app code to `localStorage`/`sessionStorage`/cookies.
  Firebase's own SDK persistence is whatever its default is; the minted token is
  purely in-memory and lost on reload (and never read back into the form).
- `SessionProvider` mirrors the live credential into the module singleton
  `authHolder` (synchronously during render — see §2):
  ```ts
  setAuthHolder({
    getToken: async () => manualToken ?? (user ? await user.getIdToken() : null),
    labId: selection?.labId ?? null,
  });
  ```
  `user.getIdToken()` returns a fresh token (Firebase refreshes as needed) on every
  request. `manualToken` takes precedence over the Firebase token — `DevTokenPanel`
  shows an "Active credential" line so a pasted token overriding a live Google
  session is visible.

### Attaching to requests

The `auth` interceptor (`api/transport.ts`) runs per-request: it calls
`getAuthHolder().getToken()`, and if non-null sets `Authorization: Bearer <token>`;
if `labId` is non-null it sets `X-QLab-Lab`. Because the holder is read at call time,
a single long-lived transport always sends current credentials.

### Gating

`canQuery = (manualToken !== null || user !== null) && selection !== null`. `SlotList`
passes `enabled: canQuery` to `useQuery`, so no request fires until there is both a
credential and a lab+pool. `App` also renders `SlotList` only when `canQuery`.

### Clearing / logout

- **`signOut()`** (`SignIn` "Sign out"): sets `manualToken = null`, then
  `firebaseSignOut(auth)`. `onAuthStateChanged` fires with `null` → `user` becomes
  null → the mirror effect rewrites `authHolder.getToken` to return `null`.
- **`clear()`** (`DevTokenPanel` "Clear"): resets `manualToken` and `selection` to
  null (does **not** sign out of Firebase). The effect then drops both headers.
- No explicit cache eviction on logout — TanStack Query's default `gcTime` applies,
  and the `enabled` gate stops further fetches once `canQuery` is false.

## Constraints captured for future work

- Generated `protogen/` is committed and regenerated via `mage genProto` — never
  hand-edit it; change `proto/` and regenerate.
- One context only (`SessionProvider`). New shared state should be justified against
  the "two or more consumers" rule in `frontend/CLAUDE.md` before adding context or
  lifting state.
- Auth headers are attached in exactly one place (`api/transport.ts`). Do not set
  `Authorization`/`X-QLab-Lab` from components.
- `env.ts` is the only reader of `import.meta.env`.
</content>
</invoke>
