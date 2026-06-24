//go:build testunit

// These tests pin the outbox rows the builder emits: the dedup key format (which
// governs whether a notification enqueues once or again), the event_type label, the
// recipient/actor, and the JSON payload fields. Delivery is Phase 11; what matters
// now is that the persisted row is exactly right, since a wrong dedup key would
// silently suppress or duplicate notifications.
package v1

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/store"
)

// TestRecommit checks the re-commit row: recipient is the slot owner, the dedup key
// is keyed on the slot id + new start (so the same start dedups but a later start
// re-enqueues), and the payload carries the slot's identifiers and new start.
func TestRecommit(t *testing.T) {
	actor := uuid.New()
	slot := store.Slot{
		ID:             uuid.New(),
		LabID:          uuid.New(),
		ResourcePoolID: uuid.New(),
		UserID:         uuid.New(),
		ActualStart:    time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC),
	}

	row := Builder{}.Recommit(actor, slot)

	require.Equal(t, slot.LabID, row.LabID)
	require.Equal(t, "slot_recommitted", row.EventType)
	require.Equal(t, slot.UserID, row.RecipientUserID)
	require.Equal(t, actor, row.ActorUserID)
	require.Equal(t, "recommit:"+slot.ID.String()+":"+strconv.FormatInt(slot.ActualStart.Unix(), 10), row.DedupKey)

	var p recommitPayload
	require.NoError(t, json.Unmarshal(row.Payload, &p))
	require.Equal(t, recommitPayload{
		SlotID: slot.ID, LabID: slot.LabID, ResourcePoolID: slot.ResourcePoolID,
		UserID: slot.UserID, NewStart: slot.ActualStart,
	}, p)
}

// TestPoke checks the poke row: recipient/occupant is the slot owner, actor is the
// poker, and the dedup key is keyed on the slot id + the poke instant (so repeated
// pokes are distinct), independent of the slot's own start.
func TestPoke(t *testing.T) {
	by := uuid.New()
	now := time.Date(2026, 6, 23, 10, 30, 0, 0, time.UTC)
	occ := store.Slot{
		ID:             uuid.New(),
		LabID:          uuid.New(),
		ResourcePoolID: uuid.New(),
		UserID:         uuid.New(),
	}

	row := Builder{}.Poke(by, occ, now)

	require.Equal(t, occ.LabID, row.LabID)
	require.Equal(t, "poke", row.EventType)
	require.Equal(t, occ.UserID, row.RecipientUserID)
	require.Equal(t, by, row.ActorUserID)
	require.Equal(t, "poke:"+occ.ID.String()+":"+strconv.FormatInt(now.Unix(), 10), row.DedupKey)

	var p pokePayload
	require.NoError(t, json.Unmarshal(row.Payload, &p))
	require.Equal(t, pokePayload{
		SlotID: occ.ID, LabID: occ.LabID, ResourcePoolID: occ.ResourcePoolID,
		OccupantUserID: occ.UserID, ByUserID: by,
	}, p)
}
