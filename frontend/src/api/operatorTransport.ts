import type { Interceptor } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { env } from "../env";
import { auth } from "../firebase";
import { HEADER_AUTHORIZATION } from "./headers";

// operatorAuth attaches the OPERATOR's own Firebase (Google) ID token — the
// signed-in user's token — to every DevService call. The backend checks it against
// the staging email allowlist (decision 0008); the browser never holds the operator
// secret. Distinct from the api transport, whose token is the *acting-as* user's
// minted token.
const operatorAuth: Interceptor = (next) => async (req) => {
  const token = await auth.currentUser?.getIdToken();
  if (token) {
    req.header.set(HEADER_AUTHORIZATION, `Bearer ${token}`);
  }
  return next(req);
};

// operatorTransport talks to the staging/local-only qlab.dev.v1.DevService. Same
// base URL as the public API (the operator surface is mounted on the same origin),
// but authenticated as the operator rather than the acting-as user.
export const operatorTransport = createConnectTransport({
  baseUrl: env.apiBaseUrl || window.location.origin,
  interceptors: [operatorAuth],
});
