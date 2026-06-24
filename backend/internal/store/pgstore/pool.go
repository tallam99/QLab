package pgstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/observability"
	"github.com/tallam99/qlab/backend/internal/store"
	"github.com/tallam99/qlab/backend/internal/store/pgstore/sqlcgen"
)

// lockResourcePoolSQL serializes all events on one pool. The slot FOR UPDATE locks
// existing rows, but a brand-new slot has no row to lock, so two concurrent
// CreateSlots could pick the same next priority and collide; an advisory lock keyed
// on the pool id makes even creates serialize. xact-scoped: released at commit.
const lockResourcePoolSQL = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`

// WithPool runs fn inside one transaction with the lab's RLS scope set, the pool
// serialized by an advisory lock, and the pool's live slots locked FOR UPDATE,
// then persists the returned mutation atomically (ALGORITHM §10).
func (s *Store) WithPool(ctx context.Context, labID, poolID, actorUserID uuid.UUID, fn func(store.PoolState) (store.PoolMutation, error)) (err error) {
	ctx, span := observability.Start(ctx, "store.with_pool",
		observability.LabID(labID), observability.PoolID(poolID))
	defer observability.End(span, &err)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	// Rollback is a no-op once Commit has run, so this is safe to always defer.
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if _, err := tx.Exec(ctx, setLabScopeSQL, labID.String()); err != nil {
		return fmt.Errorf("set lab scope: %w", err)
	}
	if _, err := tx.Exec(ctx, lockResourcePoolSQL, poolID.String()); err != nil {
		return fmt.Errorf("lock pool: %w", err)
	}

	liveRows, err := q.ListLiveSlotsForUpdate(ctx, sqlcgen.ListLiveSlotsForUpdateParams{LabsID: labID, ResourcePoolsID: poolID})
	if err != nil {
		return fmt.Errorf("lock live slots: %w", err)
	}
	slots := make([]store.Slot, 0, len(liveRows))
	for _, r := range liveRows {
		slot, err := slotFromLive(r)
		if err != nil {
			return err
		}
		slots = append(slots, slot)
	}

	resourceRows, err := q.ListResources(ctx, sqlcgen.ListResourcesParams{LabsID: labID, ResourcePoolsID: poolID})
	if err != nil {
		return fmt.Errorf("load resources: %w", err)
	}
	resources := make([]store.Resource, 0, len(resourceRows))
	for _, r := range resourceRows {
		kind, err := store.ResourceKindString(r.Kind)
		if err != nil {
			return fmt.Errorf("decode resource kind %q: %w", r.Kind, err)
		}
		resources = append(resources, store.Resource{
			ID: r.ResourcesID, ResourcePoolID: r.ResourcePoolsID, LabID: r.LabsID, Kind: kind, Name: r.Name,
		})
	}

	mutation, err := fn(store.PoolState{Slots: slots, Resources: resources})
	if err != nil {
		return err
	}

	for _, slot := range mutation.Slots {
		if err := q.UpsertSlot(ctx, upsertParams(slot, actorUserID)); err != nil {
			return fmt.Errorf("upsert slot %s: %w", slot.ID, err)
		}
	}
	for _, row := range mutation.Outbox {
		if err := q.InsertOutbox(ctx, outboxParams(row)); err != nil {
			return fmt.Errorf("enqueue outbox %s: %w", row.DedupKey, err)
		}
	}
	span.SetAttributes(
		observability.Count("slots_upserted", len(mutation.Slots)),
		observability.Count("outbox_enqueued", len(mutation.Outbox)),
	)

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// upsertParams maps a domain slot + the acting principal to the sqlc upsert params.
// The active-pin trigger forbids changing an ACTIVE slot's resource/start, but
// settling ACTIVE→COMPLETE writes those columns unchanged, so the trigger passes.
func upsertParams(s store.Slot, actorUserID uuid.UUID) sqlcgen.UpsertSlotParams {
	return sqlcgen.UpsertSlotParams{
		SlotsID:         s.ID,
		LabsID:          s.LabID,
		UsersID:         s.UserID,
		ResourcePoolsID: s.ResourcePoolID,
		ResourcesID:     nilUUID(s.ResourceID),
		SlotPriority:    s.Priority,
		DesiredStart:    s.DesiredStart,
		Lookahead:       s.LookaheadMinutes,
		Duration:        s.DurationMinutes,
		CommittedStart:  nilTime(s.CommittedStart),
		ActualStart:     nilTime(s.ActualStart),
		Status:          s.Status.String(),
		Note:            s.Note,
		Actor:           nilUUID(actorUserID),
	}
}

// outboxParams maps a domain outbox row to the sqlc insert params.
func outboxParams(row store.OutboxRow) sqlcgen.InsertOutboxParams {
	return sqlcgen.InsertOutboxParams{
		LabsID:          row.LabID,
		DedupKey:        row.DedupKey,
		EventType:       row.EventType,
		Payload:         row.Payload,
		RecipientUserID: nilUUID(row.RecipientUserID),
		Actor:           nilUUID(row.ActorUserID),
	}
}
