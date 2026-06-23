package httpmw

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/principal"
)

// HeaderDevUser and HeaderDevLab carry the caller's users_id and the lab they are
// acting in, in Phase 7, before real auth exists. They are a development stand-in:
// DevPrincipal reads them into the request context so handlers can be written
// against principal.FromContext now and need no change when Phase 8 replaces this
// with Firebase-JWT verification (the lab coming from a selected-lab claim/header)
// that populates the same principal.
//
// Exported so tests and the Yaak workspace reference the canonical spelling.
const (
	HeaderDevUser = "X-QLab-User"
	HeaderDevLab  = "X-QLab-Lab"
)

// DevPrincipal attaches a principal to the request context when both dev headers
// are present. It is intentionally permissive: it never rejects a request (health
// probes and unauthenticated calls must still flow through), so absence of a
// principal is enforced downstream by the handlers that require one. This is the
// Phase-7 stand-in for the Phase-8 auth middleware — and like the dev-login path,
// it must be compiled/guarded off outside local/staging (Phase 8).
func DevPrincipal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, userErr := uuid.Parse(r.Header.Get(HeaderDevUser))
		labID, labErr := uuid.Parse(r.Header.Get(HeaderDevLab))
		// Both headers must be present and valid uuids to form a principal; otherwise
		// the request is treated as unauthenticated (handlers reject it).
		if userErr == nil && labErr == nil {
			ctx := principal.NewContext(r.Context(), principal.Principal{UserID: userID, LabID: labID})
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}
