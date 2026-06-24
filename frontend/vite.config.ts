import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// Vite + Vitest config. Vitest reads the same config, so the dev server, the
// production build, and the test runner share one source of truth.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  // The Phase 3 hello-world placeholder still lives in public/ (deployed by
  // build.sh until PR2 swaps the CD step to `vite build`). Disabling publicDir
  // keeps `vite build` from clashing with public/index.html in the meantime.
  publicDir: false,
  server: { port: 5173 },
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: "./src/setupTests.ts",
    // The generated protobuf code is exercised by the backend; don't re-test it here.
    exclude: ["**/node_modules/**", "src/protogen/**"],
  },
});
