// Package scheduling is the domain service that turns API events into engine
// reschedules and persisted state. It is the orchestration layer between the
// transport (internal/api, which converts proto <-> domain) and the things it
// depends on through interfaces: the persistence (store.Store), the pure
// scheduling engine (dynamicqueue.Algorithm), authorization (authz.Authorizer),
// and notification building (notifications.Builder). It owns the rule that every
// mutating event mutates state, runs the single reschedule, persists the whole
// result in one transaction, and enqueues notifications to the outbox (ALGORITHM
// §5, §6, §10).
//
// Only data crosses the package boundaries as structs (store.Slot, the Result
// below); behaviour is always reached through interfaces. The implementation is in
// scheduling/v1.
package scheduling

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/principal"
	"github.com/tallam99/qlab/backend/internal/store"
)

// Domain errors. The transport maps each to a Connect code; callers compare with
// errors.Is.
var (
	// ErrNotMember: the caller is not a member of the lab they are acting in.
	ErrNotMember = errors.New("scheduling: caller is not a member of the lab")
	// ErrNotFound: the target pool or slot does not exist in the caller's lab.
	ErrNotFound = errors.New("scheduling: not found")
	// ErrForbidden: the caller may not act on this slot (not theirs, or not the
	// next-in-line user for a reclaim).
	ErrForbidden = errors.New("scheduling: not permitted")
	// ErrInvalidState: the slot is not in a state this event accepts (e.g. clocking
	// in a slot that is not SCHEDULED, reclaiming one whose grace has not lapsed).
	ErrInvalidState = errors.New("scheduling: slot is not in a valid state for this action")
)

// Clock returns the current instant. Injected so time-dependent behaviour
// (overrun, clock-in grace, pull-forward) is deterministic in tests; production
// uses time.Now.
type Clock func() time.Time

// CreateParams is the input to CreateSlot. The booker (user) and the lab come from
// the principal; slot_priority is assigned by the service. Structural validity
// (positive duration, non-negative lookahead, a desired start) is enforced at the
// transport edge by the protovalidate interceptor, so it is assumed here.
type CreateParams struct {
	ResourcePoolID   uuid.UUID
	DesiredStart     time.Time
	LookaheadMinutes int32
	DurationMinutes  int32
	Note             string
}

// Position is the engine's verdict for one SCHEDULED slot in a reschedule — where
// it landed and the notify/reclaim flags — surfaced to the caller.
type Position struct {
	SlotID             uuid.UUID
	ActualStart        time.Time
	AssignedResourceID uuid.UUID
	Recommitted        bool
	Reclaimable        bool
}

// Result is a pool's schedule after an event: the live slots (the queue) plus the
// per-slot engine verdicts from this run.
type Result struct {
	ResourcePoolID uuid.UUID
	Slots          []store.Slot
	Positions      []Position
}

// Service is the scheduling API the transport calls. Each method authenticates the
// principal against the lab, then performs its event. The names say what is acted
// on and to whom it happens. The mutating methods return the recomputed Result;
// PokeOccupant only sends a nudge.
type Service interface {
	ListSlots(ctx context.Context, p principal.Principal, poolID uuid.UUID) ([]store.Slot, error)
	CreateSlot(ctx context.Context, p principal.Principal, params CreateParams) (Result, error)
	ClockUserIn(ctx context.Context, p principal.Principal, slotID uuid.UUID) (Result, error)
	ClockUserOut(ctx context.Context, p principal.Principal, slotID uuid.UUID) (Result, error)
	CancelSlot(ctx context.Context, p principal.Principal, slotID uuid.UUID) (Result, error)
	PokeOccupant(ctx context.Context, p principal.Principal, slotID uuid.UUID) error
	ForceClockUserOut(ctx context.Context, p principal.Principal, slotID uuid.UUID) (Result, error)
	ForceUserNoShow(ctx context.Context, p principal.Principal, slotID uuid.UUID) (Result, error)
}
