package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tallam99/qlab/backend/internal/store"
)

// querier is the subset of pgx shared by the pool and a transaction, so the read
// helpers run identically standalone or inside WithPool's tx.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// slotColumns is the slots projection every slot read shares, in the order
// scanSlot expects. status is rendered as text so it scans into the SlotStatus
// enum's string form.
const slotColumns = `slots_id, labs_id, users_id, resource_pools_id, resources_id,
	slot_priority, status::text, desired_start, lookahead, duration,
	committed_start, actual_start, note`

// scanSlot reads one slots row into a store.Slot, translating SQL NULLs to the
// domain's zero values (empty ResourceID, zero CommittedStart/ActualStart) and the
// status label to the SlotStatus enum.
func scanSlot(row pgx.Row) (store.Slot, error) {
	var s store.Slot
	var resourceID *string
	var committed, actual *time.Time
	var statusText string
	if err := row.Scan(
		&s.ID, &s.LabID, &s.UserID, &s.ResourcePoolID, &resourceID,
		&s.Priority, &statusText, &s.DesiredStart, &s.LookaheadMinutes, &s.DurationMinutes,
		&committed, &actual, &s.Note,
	); err != nil {
		return store.Slot{}, err
	}
	if resourceID != nil {
		s.ResourceID = *resourceID
	}
	if committed != nil {
		s.CommittedStart = *committed
	}
	if actual != nil {
		s.ActualStart = *actual
	}
	status, err := store.SlotStatusString(statusText)
	if err != nil {
		return store.Slot{}, fmt.Errorf("decode slot status %q: %w", statusText, err)
	}
	s.Status = status
	return s, nil
}

// IsMember reports whether userID belongs to labID.
func (s *Store) IsMember(ctx context.Context, labID, userID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM labs_users WHERE labs_id = $1 AND users_id = $2)`,
		labID, userID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return ok, nil
}

// PoolByID loads a pool within labID. A missing pool (or one in another lab) is
// store.ErrNotFound.
func (s *Store) PoolByID(ctx context.Context, labID, poolID string) (store.Pool, error) {
	var p store.Pool
	var kindText string
	err := s.pool.QueryRow(ctx,
		`SELECT resource_pools_id, labs_id, kind::text, name
		 FROM resource_pools WHERE labs_id = $1 AND resource_pools_id = $2`,
		labID, poolID).Scan(&p.ID, &p.LabID, &kindText, &p.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Pool{}, store.ErrNotFound
	}
	if err != nil {
		return store.Pool{}, fmt.Errorf("load pool: %w", err)
	}
	kind, err := store.ResourceKindString(kindText)
	if err != nil {
		return store.Pool{}, fmt.Errorf("decode resource kind %q: %w", kindText, err)
	}
	p.Kind = kind
	return p, nil
}

// SlotByID loads a single slot within labID. Absent (or cross-lab) is
// store.ErrNotFound.
func (s *Store) SlotByID(ctx context.Context, labID, slotID string) (store.Slot, error) {
	slot, err := scanSlot(s.pool.QueryRow(ctx,
		`SELECT `+slotColumns+` FROM slots WHERE labs_id = $1 AND slots_id = $2`,
		labID, slotID))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Slot{}, store.ErrNotFound
	}
	if err != nil {
		return store.Slot{}, fmt.Errorf("load slot: %w", err)
	}
	return slot, nil
}

// ListSlots returns the pool's slots (full lifecycle) scoped to labID, ordered by
// desired start for a stable display order.
func (s *Store) ListSlots(ctx context.Context, labID, poolID string) ([]store.Slot, error) {
	return querySlots(ctx, s.pool,
		`SELECT `+slotColumns+` FROM slots
		 WHERE labs_id = $1 AND resource_pools_id = $2
		 ORDER BY desired_start, slots_id`,
		labID, poolID)
}

// querySlots runs a slot query on any querier and scans the full result set.
func querySlots(ctx context.Context, q querier, sql string, args ...any) ([]store.Slot, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query slots: %w", err)
	}
	defer rows.Close()
	var slots []store.Slot
	for rows.Next() {
		slot, err := scanSlot(rows)
		if err != nil {
			return nil, err
		}
		slots = append(slots, slot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate slots: %w", err)
	}
	return slots, nil
}

// queryResources loads a pool's resources, scoped to labID.
func queryResources(ctx context.Context, q querier, labID, poolID string) ([]store.Resource, error) {
	rows, err := q.Query(ctx,
		`SELECT resources_id, resource_pools_id, labs_id, kind::text, name
		 FROM resources WHERE labs_id = $1 AND resource_pools_id = $2
		 ORDER BY resources_id`,
		labID, poolID)
	if err != nil {
		return nil, fmt.Errorf("query resources: %w", err)
	}
	defer rows.Close()
	var resources []store.Resource
	for rows.Next() {
		var r store.Resource
		var kindText string
		if err := rows.Scan(&r.ID, &r.ResourcePoolID, &r.LabID, &kindText, &r.Name); err != nil {
			return nil, fmt.Errorf("scan resource: %w", err)
		}
		kind, err := store.ResourceKindString(kindText)
		if err != nil {
			return nil, fmt.Errorf("decode resource kind %q: %w", kindText, err)
		}
		r.Kind = kind
		resources = append(resources, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate resources: %w", err)
	}
	return resources, nil
}
