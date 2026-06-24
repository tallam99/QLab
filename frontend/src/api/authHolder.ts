// A tiny mutable holder bridging React auth state to the Connect transport
// interceptor. The transport is created once at module load, but the token and
// selected lab change as the user signs in / switches; rather than rebuild the
// transport, the interceptor reads the latest values from here. The session
// provider is the sole writer (via setAuthHolder).

export type TokenGetter = () => Promise<string | null>;

interface AuthHolder {
  // getToken returns a fresh ID token (Firebase refreshes it as needed) or a
  // manually pasted token; null when unauthenticated.
  getToken: TokenGetter;
  // labId is the X-QLab-Lab the caller is acting in; null until one is selected.
  labId: string | null;
}

const holder: AuthHolder = {
  getToken: async () => null,
  labId: null,
};

export function setAuthHolder(next: Partial<AuthHolder>): void {
  Object.assign(holder, next);
}

export function getAuthHolder(): AuthHolder {
  return holder;
}
