// Package notifications owns the shape of the system's notifications: it turns a
// domain event ("this slot re-committed", "poke this occupant") into the outbox
// row that records it, so business services don't carry dedup-key formats or
// payload encodings. The row is still written by the caller inside its own
// transaction (the transactional-outbox guarantee — the row must commit with the
// event that caused it), so this layer only *builds* rows in Phase 7. The delivery
// worker, channels, and retry/dead-letter land in Phase 11 behind this same
// package. The v1 implementation is in notifications/v1.
package notifications

import (
	"time"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/store"
)

// Builder constructs outbox rows for the notification triggers Phase 7 emits.
type Builder interface {
	// Recommit builds the notification for a slot whose start changed (§2.2): its
	// recipient is the slot's owner, the actor is whoever's event caused the
	// reschedule. The slot must already carry its new placement.
	Recommit(actor uuid.UUID, slot store.Slot) store.OutboxRow
	// Poke builds the nudge to an overrunning occupant: recipient is the occupant,
	// actor is the next-in-line user who poked. now disambiguates repeated pokes.
	Poke(byUserID uuid.UUID, occupant store.Slot, now time.Time) store.OutboxRow
}
