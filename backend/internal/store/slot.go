package store

import (
	"time"

	"github.com/google/uuid"
)

//go:generate go tool enumer -type=SlotStatus -trimprefix=SlotStatus -transform=snake-upper -output=slotstatus_enumer.go

// SlotStatus is a slot's full persisted lifecycle — broader than the engine's
// two live states (dynamicqueue.SlotStatus). The labels match the slot_status DB
// enum (snake-upper) so the value maps straight to and from the column. The zero
// value, SlotStatusUnknown, is never valid.
type SlotStatus int

const (
	SlotStatusUnknown   SlotStatus = iota // zero value; never valid
	SlotStatusScheduled                   // waiting; the engine places it
	SlotStatusActive                      // clocked in, running now; pinned to a resource
	SlotStatusComplete                    // finished (settled history)
	SlotStatusCancelled                   // cancelled (settled history)
	SlotStatusNoShow                      // clock-in grace lapsed and reclaimed (settled history)
)

// IsLive reports whether the slot is part of the engine's world (SCHEDULED or
// ACTIVE) — i.e. it is loaded for rescheduling and counts toward the live-priority
// uniqueness. Settled history (COMPLETE/CANCELLED/NO_SHOW) is not live.
func (s SlotStatus) IsLive() bool { return s == SlotStatusScheduled || s == SlotStatusActive }

// Slot is one persisted booking row (the slots table), carrying the full
// lifecycle. The scheduling layer converts the live ones to dynamicqueue.Slot for
// the engine and writes the engine's placements back here; nullable columns are
// represented by zero values (empty ResourceID, zero CommittedStart/ActualStart).
type Slot struct {
	ID             uuid.UUID
	LabID          uuid.UUID
	UserID         uuid.UUID
	ResourcePoolID uuid.UUID
	// ResourceID is the assigned resource, uuid.Nil when unassigned (NULL in the row).
	ResourceID uuid.UUID
	// Priority is slot_priority: a unique total order across the pool's live slots,
	// lower runs ahead. bigint in the row, so int64 here.
	Priority int64
	Status   SlotStatus

	DesiredStart     time.Time
	LookaheadMinutes int32
	DurationMinutes  int32

	// CommittedStart is the last start the user was notified of; zero means NULL
	// (never committed). ActualStart is the engine's current placement; zero means
	// NULL (unplaced). Always set once ACTIVE.
	CommittedStart time.Time
	ActualStart    time.Time

	Note string
}
