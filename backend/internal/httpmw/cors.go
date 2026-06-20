package httpmw

import (
	"net/http"

	"github.com/go-chi/cors"
)

// preflightMaxAgeSeconds caps how long a browser may cache a CORS preflight
// response, so origin/method changes propagate within a few minutes.
const preflightMaxAgeSeconds = 300

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
		// GET/POST cover liveness checks and (Phase 5) Connect-RPC, which is POST
		// over HTTP. When the Connect API lands, widen the header list for its
		// protocol headers (Connect-Protocol-Version, Connect-Timeout-Ms, …) or
		// adopt connectrpc.com/cors.
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Authorization", HeaderRequestID},
		ExposedHeaders:   []string{HeaderRequestID},
		AllowCredentials: false, // auth is a Bearer JWT in a header, not a cookie
		MaxAge:           preflightMaxAgeSeconds,
	})
}
