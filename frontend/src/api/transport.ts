import type { Interceptor } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { env } from "../env";
import { getAuthHolder } from "./authHolder";
import { HEADER_AUTHORIZATION, HEADER_SELECTED_LAB } from "./headers";

// auth attaches the bearer token and selected-lab header to every request,
// reading the current values from the auth holder so a single long-lived
// transport always sends fresh credentials.
const auth: Interceptor = (next) => async (req) => {
  const { getToken, labId } = getAuthHolder();
  const token = await getToken();
  if (token) {
    req.header.set(HEADER_AUTHORIZATION, `Bearer ${token}`);
  }
  if (labId) {
    req.header.set(HEADER_SELECTED_LAB, labId);
  }
  return next(req);
};

// In staging/prod the app talks to the Cloud Run API on a separate origin
// (decision 0001) — a cross-origin Connect client; CORS on the API allows this
// origin. In local dev apiBaseUrl is empty: calls go same-origin and the Vite
// proxy forwards them to the Go service (see vite.config.ts).
export const transport = createConnectTransport({
  baseUrl: env.apiBaseUrl || window.location.origin,
  interceptors: [auth],
});
