import { DevTokenPanel } from "./components/DevTokenPanel";
import { SignIn } from "./components/SignIn";
import { SlotList } from "./components/SlotList";
import { useSession } from "./session/SessionProvider";

// App is the Phase 9 shell: authenticate (Google or a minted token), select a
// lab + pool, and render one real authenticated call (ListSlots). The product UI
// — queue and timeline views, clock in/out, live updates — lands in Phase 10.
export function App() {
  const { canQuery, selection, initializing } = useSession();

  return (
    <div className="min-h-screen bg-slate-900 text-slate-100">
      <header className="flex items-center justify-between border-b border-slate-800 px-6 py-4">
        <h1 className="font-mono text-lg font-semibold text-teal-400">QLab</h1>
        <SignIn />
      </header>

      <main className="mx-auto grid max-w-3xl gap-6 px-6 py-8">
        <DevTokenPanel />
        <section>
          <h2 className="mb-3 font-mono text-sm uppercase tracking-wide text-slate-400">
            Slots {selection ? `· pool ${selection.poolId.slice(0, 8)}…` : ""}
          </h2>
          {initializing ? (
            <p className="text-slate-400">Starting…</p>
          ) : canQuery ? (
            <SlotList />
          ) : (
            <p className="text-slate-400">
              Sign in (or paste a minted token) and set a lab + pool to load slots.
            </p>
          )}
        </section>
      </main>
    </div>
  );
}
