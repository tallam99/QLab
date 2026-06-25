// Package v1 is the first implementation of scheduling.Service: it wires the pure
// engine to the store per ALGORITHM event, reaching the store, engine, authorizer,
// and notification builder through their interfaces.
package v1

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/dynamicqueue"
	"github.com/tallam99/qlab/backend/internal/logging"
	"github.com/tallam99/qlab/backend/internal/observability"
	"github.com/tallam99/qlab/backend/internal/principal"
	"github.com/tallam99/qlab/backend/internal/services/authz"
	"github.com/tallam99/qlab/backend/internal/services/notifications"
	"github.com/tallam99/qlab/backend/internal/services/scheduling"
	"github.com/tallam99/qlab/backend/internal/store"
)

// Options constructs the service. Store, Engine, Authorizer, and Notifications are
// interfaces (the interface-between-packages rule); Grace is the clock-in grace the
// service owns and passes to the engine on every run (dynamicqueue.Input.Grace).
type Options struct {
	Store         store.Store
	Engine        dynamicqueue.Algorithm
	Authorizer    authz.Authorizer
	Notifications notifications.Builder
	Clock         scheduling.Clock
	ClockInGrace  dynamicqueue.Minutes
	// Logger records the reschedule outcome of each mutating event. Optional —
	// defaults to a no-op so a caller that doesn't supply one (and tests) don't panic.
	Logger logging.Logger
}

// service is the concrete scheduling.Service. Stateless apart from its
// dependencies; safe to share across requests.
type service struct {
	store  store.Store
	engine dynamicqueue.Algorithm
	authz  authz.Authorizer
	notify notifications.Builder
	now    scheduling.Clock
	grace  dynamicqueue.Minutes
	logger logging.Logger
}

// New returns a scheduling.Service. Clock defaults to time.Now and Logger to a no-op
// when not supplied.
func New(opts Options) scheduling.Service {
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	logger := opts.Logger
	if logger == nil {
		logger = logging.Noop()
	}
	return &service{
		store:  opts.Store,
		engine: opts.Engine,
		authz:  opts.Authorizer,
		notify: opts.Notifications,
		now:    clock,
		grace:  opts.ClockInGrace,
		logger: logger,
	}
}

// authorize defers the membership decision to the authorizer, translating its
// error into the scheduling domain error the transport maps.
func (s *service) authorize(ctx context.Context, p principal.Principal) error {
	err := s.authz.RequireMember(ctx, p.UserID, p.LabID)
	if errors.Is(err, authz.ErrNotMember) {
		return scheduling.ErrNotMember
	}
	return err
}

// resolveSlot loads a slot in the caller's lab, translating store.ErrNotFound to
// the domain ErrNotFound.
func (s *service) resolveSlot(ctx context.Context, labID, slotID uuid.UUID) (store.Slot, error) {
	sl, err := s.store.SlotByID(ctx, labID, slotID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Slot{}, scheduling.ErrNotFound
	}
	if err != nil {
		return store.Slot{}, err
	}
	return sl, nil
}

// poolInLab confirms the pool exists in the caller's lab.
func (s *service) poolInLab(ctx context.Context, labID, poolID uuid.UUID) error {
	_, err := s.store.ResourcePoolByID(ctx, labID, poolID)
	if errors.Is(err, store.ErrNotFound) {
		return scheduling.ErrNotFound
	}
	return err
}

// mutate is the transactional skeleton every rescheduling event shares: open the
// pool's unit of work, hand build a private clone of the locked slots to apply the
// event to, then run the engine and persist the result atomically. The Result is
// captured out of the callback. event names the ALGORITHM §6 event (e.g. "clock_in")
// for the span and is this layer's single tracing site for all mutating events.
func (s *service) mutate(ctx context.Context, p principal.Principal, event string, poolID uuid.UUID, build func(working []store.Slot, resources []store.Resource, now time.Time) ([]store.Slot, []store.OutboxRow, error)) (result scheduling.Result, err error) {
	ctx, span := observability.Start(ctx, "scheduling."+event,
		observability.Event(event), observability.LabID(p.LabID), observability.PoolID(poolID), observability.UserID(p.UserID))
	defer observability.End(span, &err)

	now := s.now()
	err = s.store.WithPool(ctx, p.LabID, poolID, p.UserID, func(state store.PoolState) (store.PoolMutation, error) {
		working, extra, err := build(cloneSlots(state.Slots), state.Resources, now)
		if err != nil {
			return store.PoolMutation{}, err
		}
		mut, res, err := s.applyEngine(ctx, p.UserID, poolID, working, state.Resources, now, extra)
		if err != nil {
			return store.PoolMutation{}, err
		}
		result = res
		// A successful reschedule changed the pool's live schedule: tag the mutation so
		// the store emits a transactional schedule-change notification, which the realtime
		// listener fans out to subscribed SSE streams (decision 0010).
		mut.Notify = eventChangeKind[event]
		return mut, nil
	})
	if err == nil {
		s.logReschedule(ctx, event, p.LabID, poolID, result)
	}
	return result, err
}

// eventChangeKind maps a mutating event's name (the same string used for its span)
// to the schedule-change kind the store notifies subscribers with. force_clock_out
// settles the slot COMPLETE, so it shares the clocked-out kind. Every event that
// flows through mutate must appear here; an unmapped event simply pushes no live
// update (the queue is still correct on the next load). PokeOccupant is absent
// because it changes no schedule state and never calls mutate.
var eventChangeKind = map[string]store.ScheduleChangeKind{
	"create_slot":     store.ScheduleChangeSlotCreated,
	"clock_in":        store.ScheduleChangeClockedIn,
	"clock_out":       store.ScheduleChangeClockedOut,
	"cancel":          store.ScheduleChangeCancelled,
	"force_clock_out": store.ScheduleChangeClockedOut,
	"force_no_show":   store.ScheduleChangeNoShow,
}

// placement is a slot's reschedule outcome, rendered into a structured log line. It
// is the per-slot detail the engine.reschedule span deliberately does NOT carry (a
// span wants low-cardinality dimensions + counts; a variable-length list belongs in
// logs, where a request's story is filtered out and fed back). RFC3339 keeps the
// start human- and Claude-readable.
type placement struct {
	SlotID      string `json:"slot_id"`
	NewStart    string `json:"new_start"`
	ResourceID  string `json:"resource_id"`
	Recommitted bool   `json:"recommitted"`
	Reclaimable bool   `json:"reclaimable"`
}

// logReschedule records what a mutating event did to the queue, correlated to the
// trace by trace_id. The meaningful state changes — the slots whose committed start
// moved (and so triggered a notification) — log at Info; the full placement list logs
// at Debug (on locally, off in the cloud by default), for reconstructing a reschedule
// without re-running it. No live slots (e.g. a cancel that emptied the pool) logs
// nothing.
func (s *service) logReschedule(ctx context.Context, event string, labID, poolID uuid.UUID, result scheduling.Result) {
	logger := s.logger.With(
		observability.KeyEvent, event,
		observability.KeyLabID, labID.String(),
		observability.KeyResourcePool, poolID.String(),
	)
	if traceID := observability.TraceID(ctx); traceID != "" {
		logger = logger.With(observability.KeyTraceID, traceID)
	}

	all := make([]placement, 0, len(result.Positions))
	recommitted := make([]placement, 0)
	for _, pos := range result.Positions {
		pl := placement{
			SlotID:      pos.SlotID.String(),
			NewStart:    pos.ActualStart.Format(time.RFC3339),
			ResourceID:  pos.AssignedResourceID.String(),
			Recommitted: pos.Recommitted,
			Reclaimable: pos.Reclaimable,
		}
		all = append(all, pl)
		if pos.Recommitted {
			recommitted = append(recommitted, pl)
		}
	}

	if len(recommitted) > 0 {
		logger.Info("reschedule moved slots", "recommitted_count", len(recommitted), "slots", recommitted)
	}
	if len(all) > 0 {
		logger.Debug("reschedule placements", "count", len(all), "slots", all)
	}
}

// settleOwn is the shared body of the owner-driven settle events: load the
// caller's own slot, require the from-status, set the to-status, reschedule. event
// names the §6 event for the span.
func (s *service) settleOwn(ctx context.Context, p principal.Principal, slotID uuid.UUID, event string, from, to store.SlotStatus) (scheduling.Result, error) {
	if err := s.authorize(ctx, p); err != nil {
		return scheduling.Result{}, err
	}
	target, err := s.resolveSlot(ctx, p.LabID, slotID)
	if err != nil {
		return scheduling.Result{}, err
	}
	return s.mutate(ctx, p, event, target.ResourcePoolID, func(working []store.Slot, _ []store.Resource, _ time.Time) ([]store.Slot, []store.OutboxRow, error) {
		t := findSlot(working, slotID)
		if t == nil || t.Status != from {
			return nil, nil, scheduling.ErrInvalidState
		}
		if t.UserID != p.UserID {
			return nil, nil, scheduling.ErrForbidden
		}
		t.Status = to
		return working, nil, nil
	})
}

// graceLapsed reports whether a SCHEDULED slot's clock-in grace has passed:
// committed to a start, and now is past committedStart + grace (§2.3).
func (s *service) graceLapsed(sl store.Slot, now time.Time) bool {
	if sl.CommittedStart.IsZero() {
		return false
	}
	return now.After(sl.CommittedStart.Add(s.grace.Duration()))
}

// requireNextInLine checks the caller is the next-in-line user for the pool (the
// reclaim authorization), excluding excludeID from the computation.
func requireNextInLine(slots []store.Slot, excludeID, caller uuid.UUID) error {
	next, ok := nextInLineUser(slots, excludeID)
	if !ok || next != caller {
		return scheduling.ErrForbidden
	}
	return nil
}
