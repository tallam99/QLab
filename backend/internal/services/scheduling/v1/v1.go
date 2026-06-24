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
}

// New returns a scheduling.Service. Clock defaults to time.Now when not supplied.
func New(opts Options) scheduling.Service {
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	return &service{
		store:  opts.Store,
		engine: opts.Engine,
		authz:  opts.Authorizer,
		notify: opts.Notifications,
		now:    clock,
		grace:  opts.ClockInGrace,
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
		return mut, nil
	})
	return result, err
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
