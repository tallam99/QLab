package pgstore

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tallam99/qlab/backend/internal/store"
)

// WithPool runs fn inside one transaction with the lab's RLS scope set and the
// pool's live slots locked FOR UPDATE, then persists the returned mutation
// atomically (ALGORITHM §10). The lock serializes concurrent events on the same
// pool, so two simultaneous clock-outs can't corrupt the schedule.
func (s *Store) WithPool(ctx context.Context, labID, poolID, actorUserID string, fn func(store.PoolState) (store.PoolMutation, error)) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	// Rollback is a no-op once Commit has run, so this is safe to always defer.
	defer func() { _ = tx.Rollback(ctx) }()

	// Scope row-level security to this lab for the whole transaction (see
	// setLabScopeSQL).
	if _, err := tx.Exec(ctx, setLabScopeSQL, labID); err != nil {
		return fmt.Errorf("set lab scope: %w", err)
	}

	// Lock the pool's live slots in priority order; history is excluded — the engine
	// only sees the live world.
	slots, err := querySlots(ctx, tx,
		`SELECT `+slotColumns+` FROM slots
		 WHERE labs_id = $1 AND resource_pools_id = $2 AND status IN ('SCHEDULED', 'ACTIVE')
		 ORDER BY slot_priority
		 FOR UPDATE`,
		labID, poolID)
	if err != nil {
		return err
	}
	resources, err := queryResources(ctx, tx, labID, poolID)
	if err != nil {
		return err
	}

	mutation, err := fn(store.PoolState{Slots: slots, Resources: resources})
	if err != nil {
		return err
	}

	for _, slot := range mutation.Slots {
		if err := upsertSlot(ctx, tx, slot, actorUserID); err != nil {
			return fmt.Errorf("upsert slot %s: %w", slot.ID, err)
		}
	}
	for _, row := range mutation.Outbox {
		if err := insertOutbox(ctx, tx, row); err != nil {
			return fmt.Errorf("enqueue outbox %s: %w", row.DedupKey, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// upsertSlot inserts a new slot or updates an existing one by id. The immutable
// columns (labs_id, users_id, created_by) are set only on insert; updated_by
// records the actor whose event caused the write. The active-pin trigger forbids
// changing an ACTIVE slot's resource/start, but settling ACTIVE→COMPLETE writes
// those columns unchanged, so the trigger's IS DISTINCT FROM check passes.
func upsertSlot(ctx context.Context, q querier, s store.Slot, actorUserID string) error {
	_, err := q.Exec(ctx,
		`INSERT INTO slots (
			slots_id, labs_id, users_id, resource_pools_id, resources_id,
			slot_priority, desired_start, lookahead, duration,
			committed_start, actual_start, status, note, created_by, updated_by
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::slot_status,$13,$14,$14)
		ON CONFLICT (slots_id) DO UPDATE SET
			resources_id      = EXCLUDED.resources_id,
			resource_pools_id = EXCLUDED.resource_pools_id,
			slot_priority     = EXCLUDED.slot_priority,
			desired_start     = EXCLUDED.desired_start,
			lookahead         = EXCLUDED.lookahead,
			duration          = EXCLUDED.duration,
			committed_start   = EXCLUDED.committed_start,
			actual_start      = EXCLUDED.actual_start,
			status            = EXCLUDED.status,
			note              = EXCLUDED.note,
			updated_by        = EXCLUDED.updated_by`,
		s.ID, s.LabID, s.UserID, s.ResourcePoolID, nullString(s.ResourceID),
		s.Priority, s.DesiredStart, s.LookaheadMinutes, s.DurationMinutes,
		nullTime(s.CommittedStart), nullTime(s.ActualStart), s.Status.String(), s.Note,
		nullString(actorUserID),
	)
	return err
}

// insertOutbox enqueues a notification, idempotent on dedup_key (a retry of the
// same logical message is a no-op). Delivery is Phase 11.
func insertOutbox(ctx context.Context, q querier, row store.OutboxRow) error {
	_, err := q.Exec(ctx,
		`INSERT INTO outbox (labs_id, dedup_key, event_type, payload, created_by, updated_by)
		 VALUES ($1, $2, $3, $4::jsonb, $5, $5)
		 ON CONFLICT (dedup_key) DO NOTHING`,
		row.LabID, row.DedupKey, row.EventType, string(row.Payload), nullString(row.ActorUserID),
	)
	return err
}

// nullString maps the domain's empty-string "absent" to a SQL NULL.
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nullTime maps the domain's zero-instant "absent" to a SQL NULL.
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// compile-time guard: *pgx.Tx and *pgxpool.Pool both satisfy querier.
var _ querier = pgx.Tx(nil)
