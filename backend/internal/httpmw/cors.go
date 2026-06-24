package httpmw

import (
	"net/http"

	"github.com/go-chi/cors"
)

// preflightMaxAgeSeconds caps how long a browser may cache a CORS preflight
// response, so origin/method changes propagate within a few minutes.
const preflightMaxAgeSeconds = 300

// Request headers the browser Connect client sends that the API must allow, so
// the CORS preflight succeeds — a header the browser asks for that the server
// does not list makes the whole preflight fail, blocking the call.
const (
	// headerConnectProtocolVersion is sent on every Connect-protocol request.
	headerConnectProtocolVersion = "Connect-Protocol-Version"
	// headerConnectTimeoutMs is sent when the client sets a call deadline.
	headerConnectTimeoutMs = "Connect-Timeout-Ms"
	// headerSelectedLab selects the lab the caller acts in; mirrors
	// api.HeaderSelectedLab (defined here to avoid an httpmw→api import).
	headerSelectedLab = "X-QLab-Lab"
)

// CORS returns middleware that permits cross-origin browser requests from the
// given origins. The PWA (Firebase Hosting) and the API (Cloud Run) are separate
// origins (decision 0001), so the browser preflights every data call and reads
// responses only if the right headers come back; this supplies them.
//
// With no origins configured it denies all cross-origin access (same-origin
// only). That guard is deliberate: go-chi/cors treats an empty allow-list as
// "allow every origin", which must never be an *implicit* default — an
// unconfigured or misconfigured deployment should fail closed, not open.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	if len(allowedOrigins) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return cors.Handler(cors.Options{
		AllowedOrigins: allowedOrigins,
		// GET/POST cover liveness checks and Connect-RPC, which is POST over HTTP.
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		// The browser Connect client preflights these: the standard request headers,
		// our auth pair (Authorization + the selected-lab header), and the Connect
		// protocol headers. Omitting any one fails the preflight for the whole call.
		AllowedHeaders: []string{
			headerAccept,
			headerContentType,
			headerAuthorization,
			HeaderRequestID,
			headerSelectedLab,
			headerConnectProtocolVersion,
			headerConnectTimeoutMs,
		},
		ExposedHeaders:   []string{HeaderRequestID},
		AllowCredentials: false, // auth is a Bearer JWT in a header, not a cookie
		MaxAge:           preflightMaxAgeSeconds,
	})
}
