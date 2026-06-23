package store

// OutboxRow is a notification queued in the transactional outbox, written in the
// same transaction as the event that triggered it (the notifications convention /
// PLAN Phase 7). Delivery — the worker, retries, dead-lettering — lands in
// Phase 11; Phase 7 only enqueues.
type OutboxRow struct {
	LabID string
	// DedupKey makes enqueue idempotent: a retry of the same logical notification
	// reuses the key and is a no-op at the DB (UNIQUE + ON CONFLICT DO NOTHING).
	DedupKey string
	// EventType names the trigger (e.g. "slot_recommitted", "poke"); free text,
	// matching the outbox.event_type column (an open envelope, not an enum).
	EventType string
	// Payload is the JSON-encoded notification body (jsonb column).
	Payload []byte
	// ActorUserID is the principal that caused the event; written to created_by.
	ActorUserID string
}
