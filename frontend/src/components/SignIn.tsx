import { useState } from "react";
import { useSession } from "../session/SessionProvider";

// SignIn drives the real Firebase Google sign-in (against the Auth emulator
// locally). Once signed in it shows the user and a sign-out control. This is the
// production auth path; the dev token panel is the staging act-as shortcut.
export function SignIn() {
  const { user, signInWithGoogle, signOut } = useSession();
  const [error, setError] = useState<string | null>(null);

  if (user) {
    return (
      <div className="flex items-center gap-3">
        <span className="text-sm text-slate-300">{user.email ?? user.uid}</span>
        <button
          type="button"
          className="rounded bg-slate-700 px-3 py-1 text-sm hover:bg-slate-600"
          onClick={() => void signOut()}
        >
          Sign out
        </button>
      </div>
    );
  }

  return (
    <div className="flex flex-col items-end gap-1">
      <button
        type="button"
        className="rounded bg-teal-500 px-3 py-1 text-sm font-medium text-slate-900 hover:bg-teal-400"
        onClick={() => {
          setError(null);
          signInWithGoogle().catch((err) => setError(String(err)));
        }}
      >
        Sign in with Google
      </button>
      {error && <span className="text-xs text-red-400">{error}</span>}
    </div>
  );
}
