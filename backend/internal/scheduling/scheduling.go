// Package scheduling is the domain service that turns API events into engine
// reschedules and persisted state. It is the orchestration layer between the
// transport (internal/api, which converts proto <-> domain) and the two things it
// depends on through interfaces: the persistence (store.Store) and the pure
// scheduling engine (dynamicqueue.Algorithm). It owns the rule that every mutating
// event mutates state, runs the single reschedule, persists the whole result in
// one transaction, and enqueues notifications to the outbox (ALGORITHM §5, §6,
// §10).
//
// Only data crosses the package boundaries as structs (store.Slot, the Result
// below); behaviour is always reached through interfaces, so the service is unit-
// testable with a store mock and an engine stub.
package scheduling

import (
	"context"
	"errors"
	"time"

	"github.com/tallam99/qlab/backend/internal/dynamicqueue"
	"github.com/tallam99/qlab/backend/internal/principal"
	"github.com/tallam99/qlab/backend/internal/store"
)

// Domain errors. The transport maps each to a Connect code; callers compare with
// errors.Is. They keep the engine/store/HTTP details out of the API layer.
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
	// ErrInvalidArgument: a request field is missing or malformed.
	ErrInvalidArgument = errors.New("scheduling: invalid argument")
)

// Clock returns the current instant. Injected so time-dependent behaviour
// (overrun, clock-in grace, pull-forward) is deterministic in tests; production
// uses time.Now.
type Clock func() time.Time

// CreateParams is the input to CreateSlot. The booker (user) and the lab come from
// the principal; slot_priority is assigned by the service.
type CreateParams struct {
	ResourcePoolID   string
	DesiredStart     time.Time
	LookaheadMinutes int32
	DurationMinutes  int32
	Note             string
}

// Position is the engine's verdict for one SCHEDULED slot in a reschedule —
// where it landed and the notify/reclaim flags — surfaced to the caller.
type Position struct {
	SlotID             string
	ActualStart        time.Time
	AssignedResourceID string
	Recommitted        bool
	Reclaimable        bool
}

// Result is a pool's schedule after an event: the live slots (the queue) plus the
// per-slot engine verdicts from this run.
type Result struct {
	ResourcePoolID string
	Slots          []store.Slot
	Positions      []Position
}

// Service is the scheduling API the transport calls. Each method authenticates
// the principal against the lab, then performs its event. The mutating methods
// return the recomputed Result; Poke only sends a nudge.
type Service interface {
	ListSlots(ctx context.Context, p principal.Principal, poolID string) ([]store.Slot, error)
	CreateSlot(ctx context.Context, p principal.Principal, params CreateParams) (Result, error)
	ClockIn(ctx context.Context, p principal.Principal, slotID string) (Result, error)
	ClockOut(ctx context.Context, p principal.Principal, slotID string) (Result, error)
	Cancel(ctx context.Context, p principal.Principal, slotID string) (Result, error)
	Poke(ctx context.Context, p principal.Principal, slotID string) error
	ForceClockOut(ctx context.Context, p principal.Principal, slotID string) (Result, error)
	ForceNoShow(ctx context.Context, p principal.Principal, slotID string) (Result, error)
}

// Options constructs a service. Store and Engine are interfaces (the
// interface-between-packages rule); Engine must be built with the same grace this
// carries, since the service also evaluates grace itself for the reclaim guards.
type Options struct {
	Store        store.Store
	Engine       dynamicqueue.Algorithm
	Clock        Clock
	ClockInGrace dynamicqueue.Minutes
}

// service is the concrete Service. It is stateless apart from its dependencies and
// safe to share across requests.
type service struct {
	store  store.Store
	engine dynamicqueue.Algorithm
	now    Clock
	grace  dynamicqueue.Minutes
}

// New returns a Service. Clock defaults to time.Now when not supplied.
func New(opts Options) Service {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	return &service{
		store:  opts.Store,
		engine: opts.Engine,
		now:    opts.Clock,
		grace:  opts.ClockInGrace,
	}
}

// authorize checks the caller is a member of the lab they claim. RLS enforces lab
// match but not membership, so this is the real tenant-access gate.
func (s *service) authorize(ctx context.Context, p principal.Principal) error {
	if p.UserID == "" || p.LabID == "" {
		return ErrInvalidArgument
	}
	ok, err := s.store.IsMember(ctx, p.LabID, p.UserID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotMember
	}
	return nil
}

// resolveSlot loads a slot in the caller's lab, translating store.ErrNotFound to
// the domain ErrNotFound.
func (s *service) resolveSlot(ctx context.Context, labID, slotID string) (store.Slot, error) {
	sl, err := s.store.SlotByID(ctx, labID, slotID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Slot{}, ErrNotFound
	}
	if err != nil {
		return store.Slot{}, err
	}
	return sl, nil
}
