package v1

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/observability"
	"github.com/tallam99/qlab/backend/internal/principal"
	"github.com/tallam99/qlab/backend/internal/services/scheduling"
	"github.com/tallam99/qlab/backend/internal/store"
)

// ListSlots returns the pool's slots after verifying the caller belongs to the
// lab. Read-only: it reflects the schedule the last event produced.
func (s *service) ListSlots(ctx context.Context, p principal.Principal, poolID uuid.UUID) (slots []store.Slot, err error) {
	ctx, span := observability.Start(ctx, "scheduling.list_slots",
		observability.Event("list_slots"), observability.LabID(p.LabID), observability.PoolID(poolID))
	defer observability.End(span, &err)

	if err = s.authorize(ctx, p); err != nil {
		return nil, err
	}
	if err = s.poolInLab(ctx, p.LabID, poolID); err != nil {
		return nil, err
	}
	return s.store.ListSlots(ctx, p.LabID, poolID)
}

// Schedule returns the pool's current schedule, running the engine read-only against
// now. Unlike ListSlots (which reflects the last event's persisted placement), this
// re-projects against the current time — so in-progress overruns and lapsed grace are
// reflected — but writes nothing and notifies no one.
func (s *service) Schedule(ctx context.Context, p principal.Principal, poolID uuid.UUID) (result scheduling.Result, err error) {
	ctx, span := observability.Start(ctx, "scheduling.schedule",
		observability.Event("schedule"), observability.LabID(p.LabID), observability.PoolID(poolID))
	defer observability.End(span, &err)

	if err = s.authorize(ctx, p); err != nil {
		return scheduling.Result{}, err
	}
	if err = s.poolInLab(ctx, p.LabID, poolID); err != nil {
		return scheduling.Result{}, err
	}
	slots, err := s.store.ListSlots(ctx, p.LabID, poolID)
	if err != nil {
		return scheduling.Result{}, err
	}
	resources, err := s.store.ListResources(ctx, p.LabID, poolID)
	if err != nil {
		return scheduling.Result{}, err
	}
	return s.computeSchedule(ctx, poolID, slots, resources, s.now())
}

// CreateSlot books a SCHEDULED slot for the caller at the back of the queue, then
// reschedules (the new slot is placed at its earliest feasible start).
func (s *service) CreateSlot(ctx context.Context, p principal.Principal, params scheduling.CreateParams) (scheduling.Result, error) {
	if err := s.authorize(ctx, p); err != nil {
		return scheduling.Result{}, err
	}
	if err := s.poolInLab(ctx, p.LabID, params.ResourcePoolID); err != nil {
		return scheduling.Result{}, err
	}
	return s.mutate(ctx, p, "create_slot", params.ResourcePoolID, func(working []store.Slot, _ []store.Resource, _ time.Time) ([]store.Slot, []store.OutboxRow, error) {
		working = append(working, store.Slot{
			ID:               uuid.New(),
			LabID:            p.LabID,
			UserID:           p.UserID,
			ResourcePoolID:   params.ResourcePoolID,
			Priority:         nextPriority(working),
			Status:           store.SlotStatusScheduled,
			DesiredStart:     params.DesiredStart,
			LookaheadMinutes: params.LookaheadMinutes,
			DurationMinutes:  params.DurationMinutes,
			Note:             params.Note,
		})
		return working, nil, nil
	})
}

// ClockUserIn moves the caller's own SCHEDULED slot to ACTIVE, pinned to a free
// resource at now, then reschedules. It prefers the slot's provisional resource;
// if that is occupied (or it had none) it pins to any free resource, and fails if
// none is free (the caller must reclaim one first via poke/force-clock-out).
func (s *service) ClockUserIn(ctx context.Context, p principal.Principal, slotID uuid.UUID) (scheduling.Result, error) {
	if err := s.authorize(ctx, p); err != nil {
		return scheduling.Result{}, err
	}
	target, err := s.resolveSlot(ctx, p.LabID, slotID)
	if err != nil {
		return scheduling.Result{}, err
	}
	return s.mutate(ctx, p, "clock_in", target.ResourcePoolID, func(working []store.Slot, resources []store.Resource, now time.Time) ([]store.Slot, []store.OutboxRow, error) {
		t := findSlot(working, slotID)
		if t == nil || t.Status != store.SlotStatusScheduled {
			return nil, nil, scheduling.ErrInvalidState
		}
		if t.UserID != p.UserID {
			return nil, nil, scheduling.ErrForbidden
		}
		resourceID := t.ResourceID
		if resourceID == uuid.Nil || resourceHasActive(working, resourceID) {
			free, ok := firstFreeResource(resources, working)
			if !ok {
				return nil, nil, scheduling.ErrInvalidState
			}
			resourceID = free
		}
		t.Status = store.SlotStatusActive
		t.ResourceID = resourceID
		t.ActualStart = now
		return working, nil, nil
	})
}

// ClockUserOut settles the caller's own ACTIVE slot to COMPLETE (covering early
// finish), freeing its resource, then reschedules (pull-forward).
func (s *service) ClockUserOut(ctx context.Context, p principal.Principal, slotID uuid.UUID) (scheduling.Result, error) {
	return s.settleOwn(ctx, p, slotID, "clock_out", store.SlotStatusActive, store.SlotStatusComplete)
}

// CancelSlot settles the caller's own SCHEDULED or ACTIVE slot to CANCELLED,
// freeing its place, then reschedules (pull-forward).
func (s *service) CancelSlot(ctx context.Context, p principal.Principal, slotID uuid.UUID) (scheduling.Result, error) {
	if err := s.authorize(ctx, p); err != nil {
		return scheduling.Result{}, err
	}
	target, err := s.resolveSlot(ctx, p.LabID, slotID)
	if err != nil {
		return scheduling.Result{}, err
	}
	return s.mutate(ctx, p, "cancel", target.ResourcePoolID, func(working []store.Slot, _ []store.Resource, _ time.Time) ([]store.Slot, []store.OutboxRow, error) {
		t := findSlot(working, slotID)
		if t == nil || (t.Status != store.SlotStatusScheduled && t.Status != store.SlotStatusActive) {
			return nil, nil, scheduling.ErrInvalidState
		}
		if t.UserID != p.UserID {
			return nil, nil, scheduling.ErrForbidden
		}
		// For an ACTIVE slot the resource/start columns are left unchanged (the
		// active-pin trigger requires it); only the status moves.
		t.Status = store.SlotStatusCancelled
		return working, nil, nil
	})
}

// PokeOccupant nudges the user holding an overrunning slot to wrap up: it enqueues
// a poke notification but changes no schedule state. Only the next-in-line user may
// poke, and only an overrunning occupant.
func (s *service) PokeOccupant(ctx context.Context, p principal.Principal, slotID uuid.UUID) (err error) {
	ctx, span := observability.Start(ctx, "scheduling.poke",
		observability.Event("poke"), observability.LabID(p.LabID), observability.SlotID(slotID))
	defer observability.End(span, &err)

	if err = s.authorize(ctx, p); err != nil {
		return err
	}
	target, err := s.resolveSlot(ctx, p.LabID, slotID)
	if err != nil {
		return err
	}
	now := s.now()
	return s.store.WithPool(ctx, p.LabID, target.ResourcePoolID, p.UserID, func(state store.PoolState) (store.PoolMutation, error) {
		t := findSlot(state.Slots, slotID)
		if t == nil || t.Status != store.SlotStatusActive || !overrunning(*t, now) {
			return store.PoolMutation{}, scheduling.ErrInvalidState
		}
		if err := requireNextInLine(state.Slots, uuid.Nil, p.UserID); err != nil {
			return store.PoolMutation{}, err
		}
		return store.PoolMutation{Outbox: []store.OutboxRow{s.notify.Poke(p.UserID, *t, now)}}, nil
	})
}

// ForceClockUserOut boots an overrunning occupant (settling it COMPLETE) and
// reschedules. The next-in-line user calls it as the escalation after a poke.
func (s *service) ForceClockUserOut(ctx context.Context, p principal.Principal, slotID uuid.UUID) (scheduling.Result, error) {
	if err := s.authorize(ctx, p); err != nil {
		return scheduling.Result{}, err
	}
	target, err := s.resolveSlot(ctx, p.LabID, slotID)
	if err != nil {
		return scheduling.Result{}, err
	}
	return s.mutate(ctx, p, "force_clock_out", target.ResourcePoolID, func(working []store.Slot, _ []store.Resource, now time.Time) ([]store.Slot, []store.OutboxRow, error) {
		t := findSlot(working, slotID)
		if t == nil || t.Status != store.SlotStatusActive || !overrunning(*t, now) {
			return nil, nil, scheduling.ErrInvalidState
		}
		if err := requireNextInLine(working, uuid.Nil, p.UserID); err != nil {
			return nil, nil, err
		}
		t.Status = store.SlotStatusComplete
		return working, nil, nil
	})
}

// ForceUserNoShow reclaims a slot whose clock-in grace has lapsed (settling it
// NO_SHOW) and reschedules. The next-in-line user (the one behind the no-show)
// calls it; there is no prior poke, because the grace period was the warning.
func (s *service) ForceUserNoShow(ctx context.Context, p principal.Principal, slotID uuid.UUID) (scheduling.Result, error) {
	if err := s.authorize(ctx, p); err != nil {
		return scheduling.Result{}, err
	}
	target, err := s.resolveSlot(ctx, p.LabID, slotID)
	if err != nil {
		return scheduling.Result{}, err
	}
	return s.mutate(ctx, p, "force_no_show", target.ResourcePoolID, func(working []store.Slot, _ []store.Resource, now time.Time) ([]store.Slot, []store.OutboxRow, error) {
		t := findSlot(working, slotID)
		if t == nil || t.Status != store.SlotStatusScheduled || !s.graceLapsed(*t, now) {
			return nil, nil, scheduling.ErrInvalidState
		}
		// Exclude the lapsed target: the reclaimer is the user behind it.
		if err := requireNextInLine(working, slotID, p.UserID); err != nil {
			return nil, nil, err
		}
		t.Status = store.SlotStatusNoShow
		return working, nil, nil
	})
}
