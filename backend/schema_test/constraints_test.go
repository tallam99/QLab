//go:build database

package schematest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// TestConstraintsRejectBadRows checks that the DB itself — not just the service —
// makes domain-invalid rows unrepresentable. Each case performs an operation that
// must violate a constraint and asserts the resulting SQLSTATE. Everything runs in
// a rolled-back fixture transaction, so cases are isolated.
func TestConstraintsRejectBadRows(t *testing.T) {
	conn := connect(t)
	base := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		code string
		// op performs the offending operation; its returned error is asserted.
		op func(ctx context.Context, tx pgx.Tx, f fixture) error
	}{
		{
			name: "negative lookahead",
			code: codeCheckViolation,
			op: func(ctx context.Context, tx pgx.Tx, f fixture) error {
				_, err := tx.Exec(ctx, `INSERT INTO slots
					(labs_id, users_id, resource_pools_id, slot_priority, desired_start, lookahead, duration)
					VALUES ($1, $2, $3, 1, $4, -1, 60)`, f.labID, f.userID, f.poolID, base)
				return err
			},
		},
		{
			name: "non-positive duration",
			code: codeCheckViolation,
			op: func(ctx context.Context, tx pgx.Tx, f fixture) error {
				_, err := tx.Exec(ctx, `INSERT INTO slots
					(labs_id, users_id, resource_pools_id, slot_priority, desired_start, lookahead, duration)
					VALUES ($1, $2, $3, 1, $4, 0, 0)`, f.labID, f.userID, f.poolID, base)
				return err
			},
		},
		{
			name: "email must be lowercase",
			code: codeCheckViolation,
			op: func(ctx context.Context, tx pgx.Tx, _ fixture) error {
				_, err := tx.Exec(ctx, `INSERT INTO users (email) VALUES ('UPPER@example.com')`)
				return err
			},
		},
		{
			name: "audit created_by must reference a real user",
			code: codeForeignKeyViolation,
			op: func(ctx context.Context, tx pgx.Tx, _ fixture) error {
				_, err := tx.Exec(ctx,
					`INSERT INTO labs (name, created_by) VALUES ('x', gen_random_uuid())`)
				return err
			},
		},
		{
			name: "slot lab must match its pool's lab",
			code: codeForeignKeyViolation,
			op: func(ctx context.Context, tx pgx.Tx, f fixture) error {
				// A second lab whose id is used on a slot that points at the fixture
				// pool (which belongs to the fixture lab): the composite FK
				// (resource_pools_id, labs_id) can't resolve.
				var otherLab string
				require.NoError(t, tx.QueryRow(ctx,
					`INSERT INTO labs (name) VALUES ('other') RETURNING labs_id::text`).Scan(&otherLab))
				_, err := tx.Exec(ctx, `INSERT INTO slots
					(labs_id, users_id, resource_pools_id, slot_priority, desired_start, lookahead, duration)
					VALUES ($1, $2, $3, 1, $4, 0, 60)`, otherLab, f.userID, f.poolID, base)
				return err
			},
		},
		{
			name: "assigned resource must be in the slot's pool",
			code: codeForeignKeyViolation,
			op: func(ctx context.Context, tx pgx.Tx, f fixture) error {
				// A resource in a *different* pool (same lab) can't be assigned to a
				// slot in the fixture pool.
				var otherPool, otherRes string
				require.NoError(t, tx.QueryRow(ctx,
					`INSERT INTO resource_pools (labs_id, kind, name) VALUES ($1, 'VENT_HOOD', 'p2')
					 RETURNING resource_pools_id::text`, f.labID).Scan(&otherPool))
				require.NoError(t, tx.QueryRow(ctx,
					`INSERT INTO resources (resource_pools_id, labs_id, kind, name)
					 VALUES ($1, $2, 'VENT_HOOD', 'C') RETURNING resources_id::text`, otherPool, f.labID).Scan(&otherRes))
				_, err := tx.Exec(ctx, `INSERT INTO slots
					(labs_id, users_id, resource_pools_id, resources_id, slot_priority,
					 desired_start, lookahead, duration, actual_start)
					VALUES ($1, $2, $3, $4, 1, $5, 0, 60, $5)`,
					f.labID, f.userID, f.poolID, otherRes, base)
				return err
			},
		},
		{
			name: "booker must be a member of the lab",
			code: codeForeignKeyViolation,
			op: func(ctx context.Context, tx pgx.Tx, f fixture) error {
				var stranger string
				require.NoError(t, tx.QueryRow(ctx,
					`INSERT INTO users (email) VALUES (lower(gen_random_uuid()::text) || '@example.com')
					 RETURNING users_id::text`).Scan(&stranger))
				_, err := tx.Exec(ctx, `INSERT INTO slots
					(labs_id, users_id, resource_pools_id, slot_priority, desired_start, lookahead, duration)
					VALUES ($1, $2, $3, 1, $4, 0, 60)`, f.labID, stranger, f.poolID, base)
				return err
			},
		},
		{
			name: "live slot_priority is unique per pool",
			code: codeUniqueViolation,
			op: func(ctx context.Context, tx pgx.Tx, f fixture) error {
				_, err := tx.Exec(ctx, `INSERT INTO slots
					(labs_id, users_id, resource_pools_id, slot_priority, desired_start, lookahead, duration)
					VALUES ($1, $2, $3, 1, $4, 0, 60)`, f.labID, f.userID, f.poolID, base)
				require.NoError(t, err)
				_, err = tx.Exec(ctx, `INSERT INTO slots
					(labs_id, users_id, resource_pools_id, slot_priority, desired_start, lookahead, duration)
					VALUES ($1, $2, $3, 1, $4, 0, 60)`, f.labID, f.userID, f.poolID, base)
				return err
			},
		},
		{
			name: "no two live slots overlap on one resource",
			code: codeExclusionViolation,
			op: func(ctx context.Context, tx pgx.Tx, f fixture) error {
				// The exclusion constraint is DEFERRABLE INITIALLY DEFERRED, so force
				// it to check immediately within this transaction.
				_, err := tx.Exec(ctx, `SET CONSTRAINTS slots_no_resource_overlap IMMEDIATE`)
				require.NoError(t, err)
				_, err = tx.Exec(ctx, `INSERT INTO slots
					(labs_id, users_id, resource_pools_id, resources_id, slot_priority,
					 desired_start, lookahead, duration, actual_start)
					VALUES ($1, $2, $3, $4, 1, $5, 0, 60, $5)`,
					f.labID, f.userID, f.poolID, f.res1ID, base)
				require.NoError(t, err)
				// Same resource, starts 30m in → overlaps [base, base+60).
				_, err = tx.Exec(ctx, `INSERT INTO slots
					(labs_id, users_id, resource_pools_id, resources_id, slot_priority,
					 desired_start, lookahead, duration, actual_start)
					VALUES ($1, $2, $3, $4, 2, $5, 0, 60, $5)`,
					f.labID, f.userID, f.poolID, f.res1ID, base.Add(30*time.Minute))
				return err
			},
		},
		{
			name: "ACTIVE slot must have an assigned resource",
			code: codeCheckViolation,
			op: func(ctx context.Context, tx pgx.Tx, f fixture) error {
				_, err := tx.Exec(ctx, `INSERT INTO slots
					(labs_id, users_id, resource_pools_id, slot_priority, desired_start,
					 lookahead, duration, actual_start, status)
					VALUES ($1, $2, $3, 1, $4, 0, 60, $4, 'ACTIVE')`,
					f.labID, f.userID, f.poolID, base)
				return err
			},
		},
		{
			name: "ACTIVE slot must have an actual_start",
			code: codeCheckViolation,
			op: func(ctx context.Context, tx pgx.Tx, f fixture) error {
				_, err := tx.Exec(ctx, `INSERT INTO slots
					(labs_id, users_id, resource_pools_id, resources_id, slot_priority,
					 desired_start, lookahead, duration, status)
					VALUES ($1, $2, $3, $4, 1, $5, 0, 60, 'ACTIVE')`,
					f.labID, f.userID, f.poolID, f.res1ID, base)
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withFixture(t, conn, func(ctx context.Context, tx pgx.Tx, f fixture) {
				requirePgCode(t, tc.op(ctx, tx, f), tc.code)
			})
		})
	}
}

// TestValidSlotInserts is the positive control: a well-formed unplaced SCHEDULED
// slot and a well-formed placed slot both insert cleanly, so the constraints above
// aren't rejecting everything.
func TestValidSlotInserts(t *testing.T) {
	conn := connect(t)
	base := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)
	withFixture(t, conn, func(ctx context.Context, tx pgx.Tx, f fixture) {
		_, err := tx.Exec(ctx, `INSERT INTO slots
			(labs_id, users_id, resource_pools_id, slot_priority, desired_start, lookahead, duration)
			VALUES ($1, $2, $3, 1, $4, 30, 60)`, f.labID, f.userID, f.poolID, base)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `INSERT INTO slots
			(labs_id, users_id, resource_pools_id, resources_id, slot_priority,
			 desired_start, lookahead, duration, actual_start, status)
			VALUES ($1, $2, $3, $4, 2, $5, 0, 60, $5, 'ACTIVE')`,
			f.labID, f.userID, f.poolID, f.res1ID, base)
		require.NoError(t, err)
	})
}
