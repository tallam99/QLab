import { ActAsSwitcher } from "./components/ActAsSwitcher";
import { ComingSoon } from "./components/ComingSoon";
import { PoolPanel } from "./components/PoolPanel";
import { SignIn } from "./components/SignIn";
import { WorkspacePicker } from "./components/WorkspacePicker";
import { env } from "./env";
import { useSession } from "./session/SessionProvider";
import { useWorkspace } from "./workspace/WorkspaceProvider";

// App is the dev-switcher shell: sign in once as the operator (Google), provision or
// load a demo workspace, then act as any user in it and exercise the queue — all
// without re-pasting tokens. The product UI proper (queue + timeline) lands later.
export function App() {
  const { user, initializing } = useSession();
  const { workspace, error } = useWorkspace();

  // The switcher only works where the operator surface is mounted (local/staging).
  // In production it is disabled, so ship a neutral placeholder rather than a sign-in
  // to a dev tool that can't function there.
  if (!env.devSwitcherEnabled) {
    return <ComingSoon />;
  }

  return (
    <div className="min-h-screen bg-slate-900 text-slate-100">
      <header className="flex items-center justify-between border-b border-slate-800 px-6 py-4">
        <h1 className="font-mono text-lg font-semibold text-teal-400">QLab</h1>
        <SignIn />
      </header>

      <main className="mx-auto grid max-w-3xl gap-6 px-6 py-8">
        {initializing ? (
          <p className="text-slate-400">Starting…</p>
        ) : !user ? (
          <p className="text-slate-400">Sign in with Google to use the dev switcher.</p>
        ) : (
          <>
            {error && <p className="text-sm text-red-400">{error}</p>}
            <WorkspacePicker />
            {workspace && (
              <>
                <ActAsSwitcher />
                <PoolPanel />
              </>
            )}
          </>
        )}
      </main>
    </div>
  );
}
