package pgstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tallam99/qlab/backend/internal/store"
	"github.com/tallam99/qlab/backend/internal/store/pgstore/sqlcgen"
)

// setLabScopeSQL binds row-level security to a lab for the current transaction
// (decision 0005). LOCAL = tx-scoped. Every lab-scoped query runs after this so it
// works under the cloud app role (which RLS applies to); under the local superuser
// it is a harmless no-op. The GUC is text; the RLS policy casts it to uuid.
const setLabScopeSQL = `SELECT set_config('app.current_lab_id', $1, true)`

// inLabTx runs fn against the sqlc queries inside a short transaction with the
// lab's RLS scope set. Reads must do this (not just WithPool): RLS is fail-closed,
// so a query with no app.current_lab_id set sees zero rows under the app role.
func (s *Store) inLabTx(ctx context.Context, labID uuid.UUID, fn func(*sqlcgen.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin read tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, setLabScopeSQL, labID.String()); err != nil {
		return fmt.Errorf("set lab scope: %w", err)
	}
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IsMember reports whether userID belongs to labID.
func (s *Store) IsMember(ctx context.Context, labID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := s.inLabTx(ctx, labID, func(q *sqlcgen.Queries) error {
		var e error
		ok, e = q.IsMember(ctx, sqlcgen.IsMemberParams{LabsID: labID, UsersID: userID})
		return e
	})
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return ok, nil
}

// ResourcePoolByID loads a pool within labID. A missing pool (or one in another
// lab) is store.ErrNotFound.
func (s *Store) ResourcePoolByID(ctx context.Context, labID, poolID uuid.UUID) (store.ResourcePool, error) {
	var row sqlcgen.ResourcePoolByIDRow
	err := s.inLabTx(ctx, labID, func(q *sqlcgen.Queries) error {
		var e error
		row, e = q.ResourcePoolByID(ctx, sqlcgen.ResourcePoolByIDParams{LabsID: labID, ResourcePoolsID: poolID})
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ResourcePool{}, store.ErrNotFound
	}
	if err != nil {
		return store.ResourcePool{}, fmt.Errorf("load pool: %w", err)
	}
	kind, err := store.ResourceKindString(row.Kind)
	if err != nil {
		return store.ResourcePool{}, fmt.Errorf("decode resource kind %q: %w", row.Kind, err)
	}
	return store.ResourcePool{ID: row.ResourcePoolsID, LabID: row.LabsID, Kind: kind, Name: row.Name}, nil
}

// SlotByID loads a single slot within labID. Absent (or cross-lab) is
// store.ErrNotFound.
func (s *Store) SlotByID(ctx context.Context, labID, slotID uuid.UUID) (store.Slot, error) {
	var row sqlcgen.SlotByIDRow
	err := s.inLabTx(ctx, labID, func(q *sqlcgen.Queries) error {
		var e error
		row, e = q.SlotByID(ctx, sqlcgen.SlotByIDParams{LabsID: labID, SlotsID: slotID})
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Slot{}, store.ErrNotFound
	}
	if err != nil {
		return store.Slot{}, fmt.Errorf("load slot: %w", err)
	}
	return slotFromByID(row)
}

// ListSlots returns the pool's slots (full lifecycle) scoped to labID.
func (s *Store) ListSlots(ctx context.Context, labID, poolID uuid.UUID) ([]store.Slot, error) {
	var rows []sqlcgen.ListSlotsRow
	err := s.inLabTx(ctx, labID, func(q *sqlcgen.Queries) error {
		var e error
		rows, e = q.ListSlots(ctx, sqlcgen.ListSlotsParams{LabsID: labID, ResourcePoolsID: poolID})
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("list slots: %w", err)
	}
	out := make([]store.Slot, 0, len(rows))
	for _, r := range rows {
		slot, err := slotFromList(r)
		if err != nil {
			return nil, err
		}
		out = append(out, slot)
	}
	return out, nil
}

// ListResources returns a pool's resources scoped to labID.
func (s *Store) ListResources(ctx context.Context, labID, poolID uuid.UUID) ([]store.Resource, error) {
	var rows []sqlcgen.ListResourcesRow
	err := s.inLabTx(ctx, labID, func(q *sqlcgen.Queries) error {
		var e error
		rows, e = q.ListResources(ctx, sqlcgen.ListResourcesParams{LabsID: labID, ResourcePoolsID: poolID})
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("list resources: %w", err)
	}
	out := make([]store.Resource, 0, len(rows))
	for _, r := range rows {
		res, err := resourceFromList(r)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, nil
}

// resourceFromList decodes a ListResources row into a store.Resource.
func resourceFromList(r sqlcgen.ListResourcesRow) (store.Resource, error) {
	kind, err := store.ResourceKindString(r.Kind)
	if err != nil {
		return store.Resource{}, fmt.Errorf("decode resource kind %q: %w", r.Kind, err)
	}
	return store.Resource{
		ID: r.ResourcesID, ResourcePoolID: r.ResourcePoolsID, LabID: r.LabsID, Kind: kind, Name: r.Name,
	}, nil
}

// --- sqlc row -> store.Slot. The three read rows are structurally identical; each
// converter decodes the status label and maps the nullable columns. ---

func decodeStatus(label string) (store.SlotStatus, error) {
	st, err := store.SlotStatusString(label)
	if err != nil {
		return store.SlotStatusUnknown, fmt.Errorf("decode slot status %q: %w", label, err)
	}
	return st, nil
}

func slotFromByID(r sqlcgen.SlotByIDRow) (store.Slot, error) {
	st, err := decodeStatus(r.Status)
	if err != nil {
		return store.Slot{}, err
	}
	return store.Slot{
		ID: r.SlotsID, LabID: r.LabsID, UserID: r.UsersID, ResourcePoolID: r.ResourcePoolsID,
		ResourceID: derefUUID(r.ResourcesID), Priority: r.SlotPriority, Status: st,
		DesiredStart: r.DesiredStart, LookaheadMinutes: r.Lookahead, DurationMinutes: r.Duration,
		CommittedStart: derefTime(r.CommittedStart), ActualStart: derefTime(r.ActualStart), Note: r.Note,
	}, nil
}

func slotFromList(r sqlcgen.ListSlotsRow) (store.Slot, error) {
	st, err := decodeStatus(r.Status)
	if err != nil {
		return store.Slot{}, err
	}
	return store.Slot{
		ID: r.SlotsID, LabID: r.LabsID, UserID: r.UsersID, ResourcePoolID: r.ResourcePoolsID,
		ResourceID: derefUUID(r.ResourcesID), Priority: r.SlotPriority, Status: st,
		DesiredStart: r.DesiredStart, LookaheadMinutes: r.Lookahead, DurationMinutes: r.Duration,
		CommittedStart: derefTime(r.CommittedStart), ActualStart: derefTime(r.ActualStart), Note: r.Note,
	}, nil
}

func slotFromLive(r sqlcgen.ListLiveSlotsForUpdateRow) (store.Slot, error) {
	st, err := decodeStatus(r.Status)
	if err != nil {
		return store.Slot{}, err
	}
	return store.Slot{
		ID: r.SlotsID, LabID: r.LabsID, UserID: r.UsersID, ResourcePoolID: r.ResourcePoolsID,
		ResourceID: derefUUID(r.ResourcesID), Priority: r.SlotPriority, Status: st,
		DesiredStart: r.DesiredStart, LookaheadMinutes: r.Lookahead, DurationMinutes: r.Duration,
		CommittedStart: derefTime(r.CommittedStart), ActualStart: derefTime(r.ActualStart), Note: r.Note,
	}, nil
}
