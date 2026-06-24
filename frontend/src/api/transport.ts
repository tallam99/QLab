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

// The app talks to the Cloud Run API on a separate origin (decision 0001), so
// this is a cross-origin Connect client; CORS on the API allows this origin.
export const transport = createConnectTransport({
  baseUrl: env.apiBaseUrl,
  interceptors: [auth],
});
