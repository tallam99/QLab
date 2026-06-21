//go:build database

package schematest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdatedAtTrigger checks the set_updated_at trigger advances updated_at on
// every UPDATE. It persists a lab (committed), updates it in a separate
// transaction, and compares — now() is per-transaction, so two transactions see
// different timestamps. Cleans up the row it created.
func TestUpdatedAtTrigger(t *testing.T) {
	conn := connect(t)
	ctx := context.Background()

	var id string
	var createdAt, updatedAt0 time.Time
	require.NoError(t, conn.QueryRow(ctx,
		`INSERT INTO labs (name) VALUES ('touch test')
		 RETURNING labs_id::text, created_at, updated_at`).Scan(&id, &createdAt, &updatedAt0))
	t.Cleanup(func() { _, _ = conn.Exec(ctx, `DELETE FROM labs WHERE labs_id = $1`, id) })
	require.Equal(t, createdAt, updatedAt0, "updated_at should equal created_at on insert")

	time.Sleep(5 * time.Millisecond) // guarantee the next transaction's now() is later

	var updatedAt1 time.Time
	require.NoError(t, conn.QueryRow(ctx,
		`UPDATE labs SET name = 'touched' WHERE labs_id = $1 RETURNING updated_at`, id).Scan(&updatedAt1))
	assert.True(t, updatedAt1.After(updatedAt0),
		"updated_at should advance on UPDATE (was %s, now %s)", updatedAt0, updatedAt1)
}

// TestActivePinTrigger checks slots_enforce_active_pin: once a slot is ACTIVE its
// resource and start are immutable and it may only settle to COMPLETE/CANCELLED.
// All cases run in a rolled-back fixture transaction.
func TestActivePinTrigger(t *testing.T) {
	conn := connect(t)
	base := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)

	// insertActive creates and returns the id of an ACTIVE slot pinned to res1.
	insertActive := func(ctx context.Context, tx pgx.Tx, f fixture) string {
		var id string
		require.NoError(t, tx.QueryRow(ctx, `INSERT INTO slots
			(labs_id, users_id, resource_pools_id, resources_id, slot_priority,
			 desired_start, lookahead, duration, committed_start, actual_start, status)
			VALUES ($1, $2, $3, $4, 1, $5, 0, 60, $5, $5, 'ACTIVE')
			RETURNING slots_id::text`,
			f.labID, f.userID, f.poolID, f.res1ID, base).Scan(&id))
		return id
	}

	rejected := []struct {
		name string
		// mutate is the disallowed UPDATE on the ACTIVE slot.
		mutate func(ctx context.Context, tx pgx.Tx, f fixture, id string) error
	}{
		{
			name: "cannot reassign the resource",
			mutate: func(ctx context.Context, tx pgx.Tx, f fixture, id string) error {
				_, err := tx.Exec(ctx,
					`UPDATE slots SET resources_id = $1 WHERE slots_id = $2`, f.res2ID, id)
				return err
			},
		},
		{
			name: "cannot move the start",
			mutate: func(ctx context.Context, tx pgx.Tx, _ fixture, id string) error {
				_, err := tx.Exec(ctx,
					`UPDATE slots SET actual_start = $1 WHERE slots_id = $2`, base.Add(time.Hour), id)
				return err
			},
		},
		{
			name: "cannot revert to SCHEDULED",
			mutate: func(ctx context.Context, tx pgx.Tx, _ fixture, id string) error {
				_, err := tx.Exec(ctx, `UPDATE slots SET status = 'SCHEDULED' WHERE slots_id = $1`, id)
				return err
			},
		},
		{
			name: "cannot become NO_SHOW",
			mutate: func(ctx context.Context, tx pgx.Tx, _ fixture, id string) error {
				_, err := tx.Exec(ctx, `UPDATE slots SET status = 'NO_SHOW' WHERE slots_id = $1`, id)
				return err
			},
		},
	}

	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			withFixture(t, conn, func(ctx context.Context, tx pgx.Tx, f fixture) {
				id := insertActive(ctx, tx, f)
				requirePgCode(t, tc.mutate(ctx, tx, f, id), codeCheckViolation)
			})
		})
	}

	// The allowed settlement transitions must succeed.
	for _, status := range []string{"COMPLETE", "CANCELLED"} {
		t.Run("can settle to "+status, func(t *testing.T) {
			withFixture(t, conn, func(ctx context.Context, tx pgx.Tx, f fixture) {
				id := insertActive(ctx, tx, f)
				_, err := tx.Exec(ctx, `UPDATE slots SET status = $1 WHERE slots_id = $2`, status, id)
				require.NoError(t, err)
			})
		})
	}
}
