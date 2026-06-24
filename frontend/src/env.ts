// Typed, validated access to the Vite build-time environment. Centralizing it
// here keeps `import.meta.env` lookups out of the rest of the app and fails loudly
// at startup if a required variable is missing, rather than mid-request.

function required(name: string): string {
  const value = import.meta.env[name];
  // Distinguish unset from set-but-blank so a misconfigured deploy gets an accurate
  // diagnostic instead of being told a variable that IS present is "missing".
  if (value === undefined || value === "") {
    throw new Error(`environment variable ${name} is ${value === undefined ? "missing" : "empty"}`);
  }
  return value;
}

export const env = {
  // Local dev leaves this empty: the app calls the API same-origin and the Vite
  // proxy forwards to the Go service (see vite.config.ts). In a production build it
  // is required — an empty value there is a deploy misconfiguration, and silently
  // falling back to the Hosting origin (transport.ts) would 404 every RPC, so fail
  // loudly at startup like the Firebase vars do.
  apiBaseUrl: import.meta.env.PROD
    ? required("VITE_API_BASE_URL")
    : ((import.meta.env.VITE_API_BASE_URL as string | undefined) ?? ""),
  firebase: {
    apiKey: required("VITE_FIREBASE_API_KEY"),
    authDomain: required("VITE_FIREBASE_AUTH_DOMAIN"),
    projectId: required("VITE_FIREBASE_PROJECT_ID"),
    // Optional: present only locally to redirect the Auth SDK to the emulator.
    // Unset in staging/prod, where the SDK talks to real Firebase.
    authEmulatorHost: import.meta.env.VITE_FIREBASE_AUTH_EMULATOR_HOST as string | undefined,
  },
  // Whether to render the dev switcher (sign-in + provision/act-as). It only works
  // where the operator surface is mounted (local/staging), so it is **off by default**
  // — an unset value means a production build ships a neutral placeholder instead of a
  // dev tool. Set VITE_DEV_SWITCHER=true for local/staging builds only.
  devSwitcherEnabled: import.meta.env.VITE_DEV_SWITCHER === "true",
} as const;
