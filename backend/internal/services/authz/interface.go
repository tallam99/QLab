// Package authz answers authorization questions — "may this principal do this?" —
// over the application's data. It is a thin policy layer the business services
// depend on through an interface, so the rules live in one place and can grow
// (head-only actions like the invite RPC, etc.) without leaking into
// scheduling. It deliberately has no database of its own: membership/roles live in
// the main DB (the slots->labs_users foreign key depends on that), so the
// implementation reads through store.Store. The v1 implementation is in authz/v1.
package authz

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNotMember is returned when the caller is not a member of the lab they are
// acting in. Callers translate it to a permission error at the edge.
var ErrNotMember = errors.New("authz: caller is not a member of the lab")

// Authorizer decides whether a principal may act. Phase 7 has one rule (lab
// membership); role-based checks (head-only actions) land in Phase 8.
type Authorizer interface {
	// RequireMember returns nil if userID belongs to labID, else ErrNotMember (or a
	// wrapped infrastructure error).
	RequireMember(ctx context.Context, userID, labID uuid.UUID) error
}
