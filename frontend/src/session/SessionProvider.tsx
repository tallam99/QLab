import type { User } from "firebase/auth";
import { signOut as firebaseSignOut, onAuthStateChanged, signInWithPopup } from "firebase/auth";
import { type ReactNode, createContext, useContext, useEffect, useMemo, useState } from "react";
import { auth, googleProvider } from "../firebase";

// SessionProvider owns the OPERATOR identity: the Firebase (Google) user who drives
// the dev switcher. In staging that login is checked against the operator allowlist
// (decision 0008); its token authenticates the operator transport. The acting-as
// identity (which seeded user we impersonate) and the api transport's credentials
// live in WorkspaceProvider, not here.
interface SessionValue {
  // user is the signed-in operator, or null before sign-in.
  user: User | null;
  // initializing is true until Firebase first reports auth state, so the UI can show
  // a neutral "starting" rather than flashing the signed-out view.
  initializing: boolean;
  signInWithGoogle: () => Promise<void>;
  signOut: () => Promise<void>;
}

const SessionContext = createContext<SessionValue | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [initializing, setInitializing] = useState(true);

  // Track Firebase auth state once.
  useEffect(() => {
    return onAuthStateChanged(auth, (next) => {
      setUser(next);
      setInitializing(false);
    });
  }, []);

  const value = useMemo<SessionValue>(
    () => ({
      user,
      initializing,
      signInWithGoogle: async () => {
        await signInWithPopup(auth, googleProvider);
      },
      signOut: async () => {
        await firebaseSignOut(auth);
      },
    }),
    [user, initializing],
  );

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionValue {
  const value = useContext(SessionContext);
  if (value === null) {
    throw new Error("useSession must be used within a SessionProvider");
  }
  return value;
}
