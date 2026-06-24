import type { User } from "firebase/auth";
import { signOut as firebaseSignOut, onAuthStateChanged, signInWithPopup } from "firebase/auth";
import { type ReactNode, createContext, useContext, useEffect, useMemo, useState } from "react";
import { setAuthHolder } from "../api/authHolder";
import { auth, googleProvider } from "../firebase";

// Selecting which lab/pool the caller acts in. There is no public ListPools RPC
// yet (Phase 10), so the ids are supplied explicitly — from the operator
// ProvisionLab response locally, or a staging workspace.
export interface LabSelection {
  labId: string;
  poolId: string;
}

interface SessionValue {
  // user is the Firebase-authenticated user, or null. Independent of manualToken:
  // staging act-as uses a pasted token without a Firebase session.
  user: User | null;
  // manualToken is an operator-minted ID token pasted into the dev panel — the
  // "act as a seeded user without the OAuth dance" path (decision 0008). When set
  // it takes precedence over the Firebase token.
  manualToken: string | null;
  selection: LabSelection | null;
  // canQuery is true once we have both a credential and a lab+pool to scope by.
  canQuery: boolean;
  initializing: boolean;
  signInWithGoogle: () => Promise<void>;
  signOut: () => Promise<void>;
  setSelection: (selection: LabSelection) => void;
  setManualToken: (token: string | null) => void;
  clear: () => void;
}

const SessionContext = createContext<SessionValue | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [manualToken, setManualToken] = useState<string | null>(null);
  const [selection, setSelection] = useState<LabSelection | null>(null);
  const [initializing, setInitializing] = useState(true);

  // Track Firebase auth state once.
  useEffect(() => {
    return onAuthStateChanged(auth, (next) => {
      setUser(next);
      setInitializing(false);
    });
  }, []);

  // Mirror the credential + selected lab into the transport's holder so every
  // Connect call carries fresh headers without rebuilding the transport.
  useEffect(() => {
    setAuthHolder({
      getToken: async () => manualToken ?? (user ? await user.getIdToken() : null),
      labId: selection?.labId ?? null,
    });
  }, [user, manualToken, selection]);

  const value = useMemo<SessionValue>(
    () => ({
      user,
      manualToken,
      selection,
      canQuery: (manualToken !== null || user !== null) && selection !== null,
      initializing,
      signInWithGoogle: async () => {
        await signInWithPopup(auth, googleProvider);
      },
      signOut: async () => {
        setManualToken(null);
        await firebaseSignOut(auth);
      },
      setSelection,
      setManualToken,
      clear: () => {
        setManualToken(null);
        setSelection(null);
      },
    }),
    [user, manualToken, selection, initializing],
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
