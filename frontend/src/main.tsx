import { TransportProvider } from "@connectrpc/connect-query";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { transport } from "./api/transport";
import "./index.css";
import { SessionProvider } from "./session/SessionProvider";
import { WorkspaceProvider } from "./workspace/WorkspaceProvider";

const queryClient = new QueryClient();

const root = document.getElementById("root");
if (!root) {
  throw new Error("missing #root element");
}

createRoot(root).render(
  <StrictMode>
    <TransportProvider transport={transport}>
      <QueryClientProvider client={queryClient}>
        <SessionProvider>
          <WorkspaceProvider>
            <App />
          </WorkspaceProvider>
        </SessionProvider>
      </QueryClientProvider>
    </TransportProvider>
  </StrictMode>,
);
