import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// Vite + Vitest config. Vitest reads the same config, so the dev server, the
// production build, and the test runner share one source of truth.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    // In dev the browser talks to the Vite server same-origin; Vite proxies the
    // Connect RPC paths to the local Go API. This avoids cross-origin/CORS and
    // WSL2 localhost quirks locally. Staging/prod set VITE_API_BASE_URL to the
    // real cross-origin API URL, so CORS is still exercised where it matters.
    // Connect paths are "/<package>.<Service>/<method>". Proxy both the data API
    // (qlab.v1) and the local operator surface (qlab.dev.v1) — the in-app dev
    // switcher will call DevService from the browser, and "/qlab.v1." does not
    // prefix-match "/qlab.dev.v1.".
    proxy: {
      "/qlab.v1.": { target: "http://localhost:8090", changeOrigin: true },
      "/qlab.dev.v1.": { target: "http://localhost:8090", changeOrigin: true },
    },
  },
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: "./src/setupTests.ts",
    // The generated protobuf code is exercised by the backend; don't re-test it here.
    exclude: ["**/node_modules/**", "src/protogen/**"],
    // Dummy Firebase web config so env.ts/firebase.ts initialize during tests — any
    // test that imports a component transitively loads firebase.ts at module load.
    // These mirror .env.example; CI has no .env.local. No real Firebase is contacted.
    env: {
      VITE_FIREBASE_API_KEY: "demo-api-key",
      VITE_FIREBASE_AUTH_DOMAIN: "demo-qlab.firebaseapp.com",
      VITE_FIREBASE_PROJECT_ID: "demo-qlab",
      VITE_FIREBASE_AUTH_EMULATOR_HOST: "http://localhost:9099",
    },
  },
});
