import { initializeApp } from "firebase/app";
import { GoogleAuthProvider, connectAuthEmulator, getAuth } from "firebase/auth";
import { env } from "./env";

// Firebase client. The backend never sees Google credentials — it only verifies
// the ID token this SDK produces (decision 0001 / PLAN Phase 8). Locally the SDK
// is redirected at the Auth emulator so sign-in needs no real Google account.
const app = initializeApp({
  apiKey: env.firebase.apiKey,
  authDomain: env.firebase.authDomain,
  projectId: env.firebase.projectId,
});

export const auth = getAuth(app);

if (env.firebase.authEmulatorHost) {
  // disableWarnings silences the emulator banner the SDK logs on every call.
  connectAuthEmulator(auth, env.firebase.authEmulatorHost, { disableWarnings: true });
}

export const googleProvider = new GoogleAuthProvider();
