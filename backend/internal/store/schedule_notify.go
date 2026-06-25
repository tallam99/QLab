package store

import "github.com/google/uuid"

// ScheduleNotifyChannel is the Postgres LISTEN/NOTIFY channel a pool-mutating
// transaction signals on when it changes a pool's live schedule. A single
// listener per process subscribes to it and fans the notification out to the
// in-process subscribers of that pool (the SSE streams). The name is an SQL
// identifier, so it is a fixed const — never interpolated from input.
const ScheduleNotifyChannel = "qlab_schedule_changed"

// ScheduleChangeKind names what kind of event changed a pool's schedule. It is a
// persistence-domain string (the store never imports proto): the transport maps it
// to the qlab.v1.QueueEventType on the wire. The values mirror the event RPCs that
// reschedule a pool; PokeOccupant is absent because it changes no schedule state.
type ScheduleChangeKind string

const (
	ScheduleChangeSlotCreated ScheduleChangeKind = "slot_created"
	ScheduleChangeClockedIn   ScheduleChangeKind = "clocked_in"
	ScheduleChangeClockedOut  ScheduleChangeKind = "clocked_out"
	ScheduleChangeCancelled   ScheduleChangeKind = "cancelled"
	ScheduleChangeNoShow      ScheduleChangeKind = "no_show"
)

// ScheduleNotification is the JSON payload carried on ScheduleNotifyChannel: the
// lab and pool whose schedule changed, and what changed it. It is intentionally
// minimal — the listening process recomputes the current schedule itself rather
// than trusting a serialized snapshot — so it stays well under Postgres's 8000-byte
// NOTIFY payload limit regardless of pool size. pgstore marshals it; the realtime
// listener unmarshals it.
type ScheduleNotification struct {
	LabID  uuid.UUID          `json:"lab_id"`
	PoolID uuid.UUID          `json:"pool_id"`
	Kind   ScheduleChangeKind `json:"kind"`
}
