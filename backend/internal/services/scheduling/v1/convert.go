package v1

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/dynamicqueue"
	"github.com/tallam99/qlab/backend/internal/observability"
	"github.com/tallam99/qlab/backend/internal/services/scheduling"
	"github.com/tallam99/qlab/backend/internal/store"
)

// applyEngine is the shared tail of every mutating event. Given the post-event
// working set (all the pool's loaded slots, with the event's direct mutation
// already applied), it runs the engine over the live ones, writes each SCHEDULED
// slot's new placement back, and returns the PoolMutation to persist plus the
// Result to surface.
//
// Every live and settled slot is written (an upsert is idempotent; at QLab's scale
// the few extra writes are negligible). Outbox rows are emitted only for genuinely
// re-committed slots, so notifications never thrash.
// The engine itself (internal/dynamicqueue) stays pure — no tracing, no ctx — so the
// "engine.reschedule" span is opened here, one layer up, around the call (ALGORITHM
// §10). The blank result names let the existing explicit returns stand while the named
// err feeds the deferred End.
func (s *service) applyEngine(ctx context.Context, actor, poolID uuid.UUID, working []store.Slot, resources []store.Resource, now time.Time, extraOutbox []store.OutboxRow) (_ store.PoolMutation, _ scheduling.Result, err error) {
	ctx, span := observability.Start(ctx, "engine.reschedule")
	defer observability.End(span, &err)
	_ = ctx // the pure engine takes no context; ctx is held only for the span scope.

	input := dynamicqueue.Input{
		ResourcePoolID: dynamicqueue.ResourcePoolID(poolID.String()),
		Now:            now,
		Grace:          s.grace,
	}
	for _, r := range resources {
		input.Resources = append(input.Resources, dynamicqueue.Resource{
			ID:             dynamicqueue.ResourceID(r.ID.String()),
			ResourcePoolID: dynamicqueue.ResourcePoolID(r.ResourcePoolID.String()),
			Kind:           engineKind(r.Kind),
		})
	}
	for _, sl := range working {
		if !sl.Status.IsLive() {
			continue
		}
		es, err := toEngineSlot(sl, now)
		if err != nil {
			return store.PoolMutation{}, scheduling.Result{}, err
		}
		input.Slots = append(input.Slots, es)
	}

	span.SetAttributes(observability.Count("input_slots", len(input.Slots)))
	out, err := s.engine.Reschedule(input)
	if err != nil {
		return store.PoolMutation{}, scheduling.Result{}, fmt.Errorf("reschedule: %w", err)
	}

	writes := make([]store.Slot, 0, len(working))
	live := make([]store.Slot, 0, len(working))
	var positions []scheduling.Position
	outbox := extraOutbox
	recommitted := 0 // slots whose committed_start changed — the "which starts changed" span tag

	for _, sl := range working {
		switch sl.Status {
		case store.SlotStatusScheduled:
			pos, ok := out.Queue[dynamicqueue.SlotID(sl.ID.String())]
			if !ok {
				return store.PoolMutation{}, scheduling.Result{}, fmt.Errorf("engine did not place scheduled slot %s", sl.ID)
			}
			resID, err := uuid.Parse(string(pos.AssignedResource))
			if err != nil {
				return store.PoolMutation{}, scheduling.Result{}, fmt.Errorf("engine returned invalid resource id %q: %w", pos.AssignedResource, err)
			}
			sl.ActualStart = pos.ActualStart
			sl.ResourceID = resID
			if pos.Recommitted {
				// The engine's notify signal: persist the new committed start and
				// enqueue a re-commit notification (§2.2).
				sl.CommittedStart = pos.ActualStart
				outbox = append(outbox, s.notify.Recommit(actor, sl))
				recommitted++
			}
			positions = append(positions, scheduling.Position{
				SlotID:             sl.ID,
				ActualStart:        pos.ActualStart,
				AssignedResourceID: resID,
				Recommitted:        pos.Recommitted,
				Reclaimable:        pos.Reclaimable,
			})
			live = append(live, sl)
			writes = append(writes, sl)
		case store.SlotStatusActive:
			// Pinned: the engine never moves it; persist as-is (a newly clocked-in
			// slot is written here for the first time).
			live = append(live, sl)
			writes = append(writes, sl)
		default:
			// Settled this event (COMPLETE/CANCELLED/NO_SHOW): write the terminal row,
			// drop from the live queue and the engine's world.
			writes = append(writes, sl)
		}
	}

	span.SetAttributes(observability.Recommitted(recommitted))
	return store.PoolMutation{Slots: writes, Outbox: outbox},
		scheduling.Result{ResourcePoolID: poolID, Slots: live, Positions: positions},
		nil
}

// toEngineSlot converts a live store.Slot to the engine's Slot (opaque string ids).
// An ACTIVE slot's projected end is max(scheduledEnd, now): while it runs normally
// that is its scheduled end; once overrunning it is now ("frees imminently"), which
// the engine accepts (see dynamicqueue.Input.Validate).
func toEngineSlot(sl store.Slot, now time.Time) (dynamicqueue.Slot, error) {
	es := dynamicqueue.Slot{
		ID:             dynamicqueue.SlotID(sl.ID.String()),
		UserID:         dynamicqueue.UserID(sl.UserID.String()),
		LabID:          dynamicqueue.LabID(sl.LabID.String()),
		ResourcePoolID: dynamicqueue.ResourcePoolID(sl.ResourcePoolID.String()),
		SlotPriority:   dynamicqueue.SlotPriority(sl.Priority),
		DesiredStart:   sl.DesiredStart,
		Lookahead:      dynamicqueue.Minutes(sl.LookaheadMinutes),
		Duration:       dynamicqueue.Minutes(sl.DurationMinutes),
		CommittedStart: sl.CommittedStart,
		Note:           sl.Note,
	}
	switch sl.Status {
	case store.SlotStatusScheduled:
		es.Status = dynamicqueue.SlotStatusScheduled
	case store.SlotStatusActive:
		es.Status = dynamicqueue.SlotStatusActive
		es.AssignedResource = dynamicqueue.ResourceID(sl.ResourceID.String())
		es.ActualStart = sl.ActualStart
		es.ProjectedEnd = laterOf(sl.ActualStart.Add(dynamicqueue.Minutes(sl.DurationMinutes).Duration()), now)
	default:
		return dynamicqueue.Slot{}, fmt.Errorf("scheduling: slot %s status %s is not live", sl.ID, sl.Status)
	}
	return es, nil
}

// engineKind maps the persistence resource kind to the engine's. The engine is
// kind-agnostic, so an unmapped kind is harmless, but keep the mapping explicit.
func engineKind(k store.ResourceKind) dynamicqueue.ResourceKind {
	if k == store.ResourceKindVentHood {
		return dynamicqueue.ResourceKindVentHood
	}
	return dynamicqueue.ResourceKindUnknown
}
