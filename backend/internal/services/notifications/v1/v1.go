// Package v1 is the first implementation of notifications.Builder: it encodes each
// trigger as a JSON payload and a dedup key on a store.OutboxRow.
package v1

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/services/notifications"
	"github.com/tallam99/qlab/backend/internal/store"
)

// Builder implements notifications.Builder. It is stateless.
type Builder struct{}

// Compile-time guarantee of interface satisfaction.
var _ notifications.Builder = Builder{}

// New returns a notifications Builder.
func New() Builder { return Builder{} }

// recommitPayload is the body of a re-commit notification (the slot's start
// changed). A domain struct, JSON-encoded into the outbox — the proto envelope is
// a transport concern; Phase 11 owns delivery.
type recommitPayload struct {
	SlotID         uuid.UUID `json:"slot_id"`
	LabID          uuid.UUID `json:"lab_id"`
	ResourcePoolID uuid.UUID `json:"resource_pool_id"`
	UserID         uuid.UUID `json:"user_id"`
	NewStart       time.Time `json:"new_start"`
}

// Recommit builds the outbox row for a re-committed slot. The dedup key is keyed
// on the new start, so the same notification enqueues once but a later, different
// start enqueues again.
func (Builder) Recommit(actor uuid.UUID, slot store.Slot) store.OutboxRow {
	payload, _ := json.Marshal(recommitPayload{
		SlotID:         slot.ID,
		LabID:          slot.LabID,
		ResourcePoolID: slot.ResourcePoolID,
		UserID:         slot.UserID,
		NewStart:       slot.ActualStart,
	})
	return store.OutboxRow{
		LabID:           slot.LabID,
		DedupKey:        fmt.Sprintf("recommit:%s:%d", slot.ID, slot.ActualStart.Unix()),
		EventType:       "slot_recommitted",
		Payload:         payload,
		RecipientUserID: slot.UserID,
		ActorUserID:     actor,
	}
}

// pokePayload is the body of a poke notification.
type pokePayload struct {
	SlotID         uuid.UUID `json:"slot_id"`
	LabID          uuid.UUID `json:"lab_id"`
	ResourcePoolID uuid.UUID `json:"resource_pool_id"`
	OccupantUserID uuid.UUID `json:"occupant_user_id"`
	ByUserID       uuid.UUID `json:"by_user_id"`
}

// Poke builds the outbox row for a poke. The dedup key includes the instant (to
// the second) so repeated pokes are distinct notifications.
func (Builder) Poke(byUserID uuid.UUID, occupant store.Slot, now time.Time) store.OutboxRow {
	payload, _ := json.Marshal(pokePayload{
		SlotID:         occupant.ID,
		LabID:          occupant.LabID,
		ResourcePoolID: occupant.ResourcePoolID,
		OccupantUserID: occupant.UserID,
		ByUserID:       byUserID,
	})
	return store.OutboxRow{
		LabID:           occupant.LabID,
		DedupKey:        fmt.Sprintf("poke:%s:%d", occupant.ID, now.Unix()),
		EventType:       "poke",
		Payload:         payload,
		RecipientUserID: occupant.UserID,
		ActorUserID:     byUserID,
	}
}
