package scheduling

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/tallam99/qlab/backend/internal/dynamicqueue"
	"github.com/tallam99/qlab/backend/internal/store"
)

// applyEngine is the shared tail of every mutating event. Given the post-event
// working set (all the pool's loaded slots, with the event's direct mutation
// already applied), it runs the engine over the live ones, writes each SCHEDULED
// slot's new placement back, and returns the PoolMutation to persist plus the
// Result to surface.
//
// Every live and settled slot is written (an upsert is idempotent; at QLab's scale
// the few extra writes are negligible — diffing to skip unchanged rows is a noted
// future optimization). Outbox rows are emitted only for genuinely re-committed
// slots, so notifications never thrash.
func (s *service) applyEngine(labID, poolID, actor string, working []store.Slot, resources []store.Resource, now time.Time, extraOutbox []store.OutboxRow) (store.PoolMutation, Result, error) {
	input, err := toEngineInput(poolID, working, resources, now)
	if err != nil {
		return store.PoolMutation{}, Result{}, err
	}
	out, err := s.engine.Reschedule(input)
	if err != nil {
		return store.PoolMutation{}, Result{}, fmt.Errorf("reschedule: %w", err)
	}

	writes := make([]store.Slot, 0, len(working))
	live := make([]store.Slot, 0, len(working))
	var positions []Position
	outbox := extraOutbox

	for _, sl := range working {
		switch sl.Status {
		case store.SlotStatusScheduled:
			pos, ok := out.Queue[dynamicqueue.SlotID(sl.ID)]
			if !ok {
				return store.PoolMutation{}, Result{}, fmt.Errorf("engine did not place scheduled slot %s", sl.ID)
			}
			sl.ActualStart = pos.ActualStart
			sl.ResourceID = string(pos.AssignedResource)
			if pos.Recommitted {
				// The engine's notify signal: persist the new committed start and
				// enqueue a re-commit notification (§2.2).
				sl.CommittedStart = pos.ActualStart
				outbox = append(outbox, recommitOutbox(labID, poolID, actor, sl))
			}
			positions = append(positions, Position{
				SlotID:             sl.ID,
				ActualStart:        pos.ActualStart,
				AssignedResourceID: string(pos.AssignedResource),
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

	return store.PoolMutation{Slots: writes, Outbox: outbox},
		Result{ResourcePoolID: poolID, Slots: live, Positions: positions},
		nil
}

// toEngineInput converts the live slots and the pool's resources into the engine's
// Input. History is filtered out (the engine sees only the live world, §1.2).
func toEngineInput(poolID string, slots []store.Slot, resources []store.Resource, now time.Time) (dynamicqueue.Input, error) {
	in := dynamicqueue.Input{
		ResourcePoolID: dynamicqueue.ResourcePoolID(poolID),
		Now:            now,
	}
	for _, r := range resources {
		in.Resources = append(in.Resources, dynamicqueue.Resource{
			ID:             dynamicqueue.ResourceID(r.ID),
			ResourcePoolID: dynamicqueue.ResourcePoolID(r.ResourcePoolID),
			Kind:           engineKind(r.Kind),
		})
	}
	for _, sl := range slots {
		if !sl.Status.IsLive() {
			continue
		}
		es, err := toEngineSlot(sl, now)
		if err != nil {
			return dynamicqueue.Input{}, err
		}
		in.Slots = append(in.Slots, es)
	}
	return in, nil
}

// toEngineSlot converts a live store.Slot to the engine's Slot. An ACTIVE slot's
// projected end is max(scheduledEnd, now): while it runs normally that is its
// scheduled end; once overrunning it is now ("frees imminently"), which the engine
// accepts (see dynamicqueue.Input.Validate).
func toEngineSlot(sl store.Slot, now time.Time) (dynamicqueue.Slot, error) {
	es := dynamicqueue.Slot{
		ID:             dynamicqueue.SlotID(sl.ID),
		UserID:         dynamicqueue.UserID(sl.UserID),
		LabID:          dynamicqueue.LabID(sl.LabID),
		ResourcePoolID: dynamicqueue.ResourcePoolID(sl.ResourcePoolID),
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
		es.AssignedResource = dynamicqueue.ResourceID(sl.ResourceID)
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

// recommitPayload is the notification body for a re-committed slot (its start
// changed). A domain struct, JSON-encoded into the outbox — the proto envelope is
// a transport concern and stays out of this layer; Phase 11 owns delivery.
type recommitPayload struct {
	SlotID         string    `json:"slot_id"`
	LabID          string    `json:"lab_id"`
	ResourcePoolID string    `json:"resource_pool_id"`
	UserID         string    `json:"user_id"`
	NewStart       time.Time `json:"new_start"`
}

// recommitOutbox builds the outbox row for a re-committed slot. The dedup key is
// keyed on the new start so the same notification is enqueued once but a later,
// different start enqueues again.
func recommitOutbox(labID, poolID, actor string, sl store.Slot) store.OutboxRow {
	payload, _ := json.Marshal(recommitPayload{
		SlotID:         sl.ID,
		LabID:          labID,
		ResourcePoolID: poolID,
		UserID:         sl.UserID,
		NewStart:       sl.ActualStart,
	})
	return store.OutboxRow{
		LabID:       labID,
		DedupKey:    fmt.Sprintf("recommit:%s:%d", sl.ID, sl.ActualStart.Unix()),
		EventType:   "slot_recommitted",
		Payload:     payload,
		ActorUserID: actor,
	}
}

// pokePayload is the notification body for a poke (a nudge to an overrunning
// occupant).
type pokePayload struct {
	SlotID         string `json:"slot_id"`
	LabID          string `json:"lab_id"`
	ResourcePoolID string `json:"resource_pool_id"`
	OccupantUserID string `json:"occupant_user_id"`
	ByUserID       string `json:"by_user_id"`
}

// pokeOutbox builds the outbox row for a poke. The dedup key includes the instant
// (to the second) so repeated pokes are distinct notifications.
func pokeOutbox(labID, poolID, byUserID string, occupant store.Slot, now time.Time) store.OutboxRow {
	payload, _ := json.Marshal(pokePayload{
		SlotID:         occupant.ID,
		LabID:          labID,
		ResourcePoolID: poolID,
		OccupantUserID: occupant.UserID,
		ByUserID:       byUserID,
	})
	return store.OutboxRow{
		LabID:       labID,
		DedupKey:    fmt.Sprintf("poke:%s:%d", occupant.ID, now.Unix()),
		EventType:   "poke",
		Payload:     payload,
		ActorUserID: byUserID,
	}
}
