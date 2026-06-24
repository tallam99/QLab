// Typed, validated access to the Vite build-time environment. Centralizing it
// here keeps `import.meta.env` lookups out of the rest of the app and fails loudly
// at startup if a required variable is missing, rather than mid-request.

function required(name: string): string {
  const value = import.meta.env[name];
  if (!value) {
    throw new Error(`missing required environment variable ${name}`);
  }
  return value;
}

export const env = {
  // Empty in local dev: the app calls the API same-origin and the Vite proxy
  // forwards to the Go service (see vite.config.ts). Set to the full cross-origin
  // API URL in staging/prod.
  apiBaseUrl: (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? "",
  firebase: {
    apiKey: required("VITE_FIREBASE_API_KEY"),
    authDomain: required("VITE_FIREBASE_AUTH_DOMAIN"),
    projectId: required("VITE_FIREBASE_PROJECT_ID"),
    // Optional: present only locally to redirect the Auth SDK to the emulator.
    // Unset in staging/prod, where the SDK talks to real Firebase.
    authEmulatorHost: import.meta.env.VITE_FIREBASE_AUTH_EMULATOR_HOST as string | undefined,
  },
} as const;
