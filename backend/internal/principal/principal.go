// Package principal carries the authenticated caller's identity through the
// request context. It is the seam between the transport's auth mechanism and the
// handlers: handlers read a Principal, never the raw credential.
//
// In Phase 7 there is no real auth yet, so the principal is resolved from a dev
// header (see httpmw.DevPrincipal). Phase 8 swaps that for Firebase-JWT
// verification but populates the SAME Principal shape, so handlers do not change.
//
// A user can belong to several labs, so the Principal also carries the lab the
// caller is acting in (chosen by the UI; a dev header in Phase 7, a selected-lab
// claim/header in Phase 8). Carrying it up front is required, not cosmetic: the
// database's row-level security is fail-closed and needs app.current_lab_id set
// *before* any read, so a lab can't be discovered from a bare resource id. RLS
// only checks the lab matches — it does NOT check membership — so handlers must
// still verify the caller is a member of the lab (store.IsMember).
package principal

import (
	"context"

	"github.com/google/uuid"
)

// Principal is the authenticated caller and the lab they are acting in. The ids
// are parsed at the transport edge, so they are uuids by the time any handler runs.
type Principal struct {
	// UserID is the caller's users_id (the booker/actor for an event).
	UserID uuid.UUID
	// LabID is the lab the request operates within. Every Phase-7 RPC is
	// lab-scoped, so both fields are required for an authenticated principal.
	LabID uuid.UUID
}

// ctxKey is an unexported context key type so no other package can collide with
// or overwrite the stored principal.
type ctxKey struct{}

// NewContext returns a copy of ctx carrying p.
func NewContext(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext returns the principal stored in ctx, if any. The boolean is false
// when the request was not authenticated (no principal was attached), which
// handlers translate into an Unauthenticated error.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}
