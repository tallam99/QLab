package scheduling

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/principal"
	"github.com/tallam99/qlab/backend/internal/store"
)

// ListSlots returns the pool's slots, after verifying the caller belongs to the
// lab. Read-only: it reflects the schedule the last event produced.
func (s *service) ListSlots(ctx context.Context, p principal.Principal, poolID string) ([]store.Slot, error) {
	if err := s.authorize(ctx, p); err != nil {
		return nil, err
	}
	if _, err := s.poolInLab(ctx, p.LabID, poolID); err != nil {
		return nil, err
	}
	return s.store.ListSlots(ctx, p.LabID, poolID)
}

// CreateSlot books a SCHEDULED slot for the caller at the back of the queue, then
// reschedules (the new slot is placed at its earliest feasible start).
func (s *service) CreateSlot(ctx context.Context, p principal.Principal, params CreateParams) (Result, error) {
	if err := s.authorize(ctx, p); err != nil {
		return Result{}, err
	}
	if params.ResourcePoolID == "" || params.DesiredStart.IsZero() ||
		params.DurationMinutes <= 0 || params.LookaheadMinutes < 0 {
		return Result{}, ErrInvalidArgument
	}
	if _, err := s.poolInLab(ctx, p.LabID, params.ResourcePoolID); err != nil {
		return Result{}, err
	}
	return s.mutate(ctx, p, params.ResourcePoolID, func(state store.PoolState, _ time.Time) ([]store.Slot, []store.OutboxRow, error) {
		working := cloneSlots(state.Slots)
		working = append(working, store.Slot{
			ID:               uuid.NewString(),
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

// ClockIn moves the caller's own SCHEDULED slot to ACTIVE, pinned to a free
// resource at now, then reschedules. It prefers the slot's provisional resource;
// if that is occupied (or it had none) it pins to any free resource, and fails if
// none is free (the caller must reclaim one first via poke/force-clock-out).
func (s *service) ClockIn(ctx context.Context, p principal.Principal, slotID string) (Result, error) {
	if err := s.authorize(ctx, p); err != nil {
		return Result{}, err
	}
	target, err := s.resolveSlot(ctx, p.LabID, slotID)
	if err != nil {
		return Result{}, err
	}
	return s.mutate(ctx, p, target.ResourcePoolID, func(state store.PoolState, now time.Time) ([]store.Slot, []store.OutboxRow, error) {
		working := cloneSlots(state.Slots)
		t := findSlot(working, slotID)
		if t == nil || t.Status != store.SlotStatusScheduled {
			return nil, nil, ErrInvalidState
		}
		if t.UserID != p.UserID {
			return nil, nil, ErrForbidden
		}
		resourceID := t.ResourceID
		if resourceID == "" || resourceHasActive(working, resourceID) {
			free, ok := firstFreeResource(state.Resources, working)
			if !ok {
				return nil, nil, ErrInvalidState
			}
			resourceID = free
		}
		t.Status = store.SlotStatusActive
		t.ResourceID = resourceID
		t.ActualStart = now
		return working, nil, nil
	})
}

// ClockOut settles the caller's own ACTIVE slot to COMPLETE (covering early
// finish), freeing its resource, then reschedules (pull-forward).
func (s *service) ClockOut(ctx context.Context, p principal.Principal, slotID string) (Result, error) {
	return s.settleOwn(ctx, p, slotID, store.SlotStatusActive, store.SlotStatusComplete)
}

// Cancel settles the caller's own SCHEDULED or ACTIVE slot to CANCELLED, freeing
// its place, then reschedules (pull-forward).
func (s *service) Cancel(ctx context.Context, p principal.Principal, slotID string) (Result, error) {
	if err := s.authorize(ctx, p); err != nil {
		return Result{}, err
	}
	target, err := s.resolveSlot(ctx, p.LabID, slotID)
	if err != nil {
		return Result{}, err
	}
	return s.mutate(ctx, p, target.ResourcePoolID, func(state store.PoolState, _ time.Time) ([]store.Slot, []store.OutboxRow, error) {
		working := cloneSlots(state.Slots)
		t := findSlot(working, slotID)
		if t == nil || (t.Status != store.SlotStatusScheduled && t.Status != store.SlotStatusActive) {
			return nil, nil, ErrInvalidState
		}
		if t.UserID != p.UserID {
			return nil, nil, ErrForbidden
		}
		// For an ACTIVE slot the resource/start columns are left unchanged (the
		// active-pin trigger requires it); only the status moves.
		t.Status = store.SlotStatusCancelled
		return working, nil, nil
	})
}

// Poke nudges the user holding an overrunning slot to wrap up: it enqueues a poke
// notification but changes no schedule state. Only the next-in-line user may poke,
// and only an overrunning occupant.
func (s *service) Poke(ctx context.Context, p principal.Principal, slotID string) error {
	if err := s.authorize(ctx, p); err != nil {
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
			return store.PoolMutation{}, ErrInvalidState
		}
		if err := requireNextInLine(state.Slots, "", p.UserID); err != nil {
			return store.PoolMutation{}, err
		}
		return store.PoolMutation{Outbox: []store.OutboxRow{pokeOutbox(p.LabID, target.ResourcePoolID, p.UserID, *t, now)}}, nil
	})
}

// ForceClockOut boots an overrunning occupant (settling it COMPLETE) and
// reschedules. The next-in-line user calls it as the escalation after a poke; the
// wait and the decision to escalate are the user's, not the backend's.
func (s *service) ForceClockOut(ctx context.Context, p principal.Principal, slotID string) (Result, error) {
	if err := s.authorize(ctx, p); err != nil {
		return Result{}, err
	}
	target, err := s.resolveSlot(ctx, p.LabID, slotID)
	if err != nil {
		return Result{}, err
	}
	return s.mutate(ctx, p, target.ResourcePoolID, func(state store.PoolState, now time.Time) ([]store.Slot, []store.OutboxRow, error) {
		working := cloneSlots(state.Slots)
		t := findSlot(working, slotID)
		if t == nil || t.Status != store.SlotStatusActive || !overrunning(*t, now) {
			return nil, nil, ErrInvalidState
		}
		if err := requireNextInLine(working, "", p.UserID); err != nil {
			return nil, nil, err
		}
		t.Status = store.SlotStatusComplete
		return working, nil, nil
	})
}

// ForceNoShow reclaims a slot whose clock-in grace has lapsed (settling it
// NO_SHOW) and reschedules. The next-in-line user (the one behind the no-show)
// calls it; there is no prior poke, because the grace period was the warning.
func (s *service) ForceNoShow(ctx context.Context, p principal.Principal, slotID string) (Result, error) {
	if err := s.authorize(ctx, p); err != nil {
		return Result{}, err
	}
	target, err := s.resolveSlot(ctx, p.LabID, slotID)
	if err != nil {
		return Result{}, err
	}
	return s.mutate(ctx, p, target.ResourcePoolID, func(state store.PoolState, now time.Time) ([]store.Slot, []store.OutboxRow, error) {
		working := cloneSlots(state.Slots)
		t := findSlot(working, slotID)
		if t == nil || t.Status != store.SlotStatusScheduled || !s.graceLapsed(*t, now) {
			return nil, nil, ErrInvalidState
		}
		// Exclude the lapsed target: the reclaimer is the user behind it.
		if err := requireNextInLine(working, slotID, p.UserID); err != nil {
			return nil, nil, err
		}
		t.Status = store.SlotStatusNoShow
		return working, nil, nil
	})
}

// settleOwn is the shared body of the owner-driven settle events (clock-out): load
// the caller's own slot, require the from-status, set the to-status, reschedule.
func (s *service) settleOwn(ctx context.Context, p principal.Principal, slotID string, from, to store.SlotStatus) (Result, error) {
	if err := s.authorize(ctx, p); err != nil {
		return Result{}, err
	}
	target, err := s.resolveSlot(ctx, p.LabID, slotID)
	if err != nil {
		return Result{}, err
	}
	return s.mutate(ctx, p, target.ResourcePoolID, func(state store.PoolState, _ time.Time) ([]store.Slot, []store.OutboxRow, error) {
		working := cloneSlots(state.Slots)
		t := findSlot(working, slotID)
		if t == nil || t.Status != from {
			return nil, nil, ErrInvalidState
		}
		if t.UserID != p.UserID {
			return nil, nil, ErrForbidden
		}
		t.Status = to
		return working, nil, nil
	})
}

// mutate is the transactional skeleton every rescheduling event shares: open the
// pool's unit-of-work, let build apply the event to a working copy of the locked
// state, then run the engine and persist the result atomically. The Result is
// captured out of the callback.
func (s *service) mutate(ctx context.Context, p principal.Principal, poolID string, build func(state store.PoolState, now time.Time) ([]store.Slot, []store.OutboxRow, error)) (Result, error) {
	now := s.now()
	var result Result
	err := s.store.WithPool(ctx, p.LabID, poolID, p.UserID, func(state store.PoolState) (store.PoolMutation, error) {
		working, extra, err := build(state, now)
		if err != nil {
			return store.PoolMutation{}, err
		}
		mut, res, err := s.applyEngine(p.LabID, poolID, p.UserID, working, state.Resources, now, extra)
		if err != nil {
			return store.PoolMutation{}, err
		}
		result = res
		return mut, nil
	})
	return result, err
}

// poolInLab confirms the pool exists in the caller's lab, mapping not-found to the
// domain error.
func (s *service) poolInLab(ctx context.Context, labID, poolID string) (store.Pool, error) {
	pool, err := s.store.PoolByID(ctx, labID, poolID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Pool{}, ErrNotFound
	}
	if err != nil {
		return store.Pool{}, err
	}
	return pool, nil
}

// graceLapsed reports whether a SCHEDULED slot's clock-in grace has passed:
// committed to a start, and now is past committedStart + grace (§2.3).
func (s *service) graceLapsed(sl store.Slot, now time.Time) bool {
	if sl.CommittedStart.IsZero() {
		return false
	}
	return now.After(sl.CommittedStart.Add(s.grace.Duration()))
}

// requireNextInLine checks caller is the next-in-line user for the pool (the
// reclaim authorization), excluding excludeID from the computation.
func requireNextInLine(slots []store.Slot, excludeID, caller string) error {
	next, ok := nextInLineUser(slots, excludeID)
	if !ok || next != caller {
		return ErrForbidden
	}
	return nil
}
