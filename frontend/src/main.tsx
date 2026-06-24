import { TransportProvider } from "@connectrpc/connect-query";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { Preview } from "./Preview";
import { transport } from "./api/transport";
import "./index.css";
import { SessionProvider } from "./session/SessionProvider";
import { applyTheme, getTheme } from "./theme";
import { WorkspaceProvider } from "./workspace/WorkspaceProvider";

// Apply the saved (or default dark) theme before first paint to avoid a flash.
applyTheme(getTheme());

const queryClient = new QueryClient();

const root = document.getElementById("root");
if (!root) {
  throw new Error("missing #root element");
}

// Dev-only component gallery for the design loop (see Preview.tsx). Gated on
// import.meta.env.DEV, so it is never reachable in a production build.
const isPreview = import.meta.env.DEV && window.location.pathname === "/preview";

createRoot(root).render(
  <StrictMode>
    {isPreview ? (
      <Preview />
    ) : (
      <TransportProvider transport={transport}>
        <QueryClientProvider client={queryClient}>
          <SessionProvider>
            <WorkspaceProvider>
              <App />
            </WorkspaceProvider>
          </SessionProvider>
        </QueryClientProvider>
      </TransportProvider>
    )}
  </StrictMode>,
);
