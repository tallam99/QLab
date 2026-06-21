//go:build database

package schematest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnumLabels checks each native enum type carries exactly the expected labels
// in order — the labels match the Go enumer string form, so the domain mapping
// (Phase 7) can rely on them.
func TestEnumLabels(t *testing.T) {
	conn := connect(t)
	ctx := context.Background()

	want := map[string][]string{
		"lab_role":      {"HEAD", "MEMBER"},
		"resource_kind": {"VENT_HOOD"},
		"slot_status":   {"SCHEDULED", "ACTIVE", "COMPLETE", "CANCELLED", "NO_SHOW"},
		"outbox_status": {"PENDING", "SENT", "DEAD"},
	}

	for typ, expected := range want {
		t.Run(typ, func(t *testing.T) {
			rows, err := conn.Query(ctx,
				`SELECT e.enumlabel
				 FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid
				 WHERE t.typname = $1
				 ORDER BY e.enumsortorder`, typ)
			require.NoError(t, err)
			defer rows.Close()

			var got []string
			for rows.Next() {
				var label string
				require.NoError(t, rows.Scan(&label))
				got = append(got, label)
			}
			require.NoError(t, rows.Err())
			assert.Equal(t, expected, got)
		})
	}
}

// Demo seed IDs (must match backend/seed/seed.sql).
const (
	demoLab    = "10000000-0000-0000-0000-000000000001"
	demoPool   = "30000000-0000-0000-0000-000000000001"
	demoResA   = "40000000-0000-0000-0000-000000000001"
	demoActive = "50000000-0000-0000-0000-000000000001"
	demoPulled = "50000000-0000-0000-0000-000000000002"
)

// TestSeedValues checks `mage testSchema` loaded the demo seed with the expected
// shape: one lab, a head + two members, a vent-hood pool of two resources, and a
// four-slot queue whose first entry is the ACTIVE one pinned to Vent Hood A.
func TestSeedValues(t *testing.T) {
	conn := connect(t)
	ctx := context.Background()

	var labName string
	require.NoError(t, conn.QueryRow(ctx,
		`SELECT name FROM labs WHERE id = $1`, demoLab).Scan(&labName))
	assert.Equal(t, "Demo Lab", labName)

	scalar := func(query string, args ...any) int {
		var n int
		require.NoError(t, conn.QueryRow(ctx, query, args...).Scan(&n))
		return n
	}

	assert.Equal(t, 3, scalar(`SELECT count(*) FROM lab_memberships WHERE lab_id = $1`, demoLab),
		"three memberships")
	assert.Equal(t, 1, scalar(`SELECT count(*) FROM lab_memberships WHERE lab_id = $1 AND role = 'HEAD'`, demoLab),
		"one head")
	assert.Equal(t, 2, scalar(`SELECT count(*) FROM lab_memberships WHERE lab_id = $1 AND role = 'MEMBER'`, demoLab),
		"two members")
	assert.Equal(t, 2, scalar(`SELECT count(*) FROM resources WHERE resource_pool_id = $1`, demoPool),
		"two resources in the pool")
	assert.Equal(t, 4, scalar(`SELECT count(*) FROM slots WHERE lab_id = $1`, demoLab),
		"four slots")

	// The first slot is ACTIVE and pinned to Vent Hood A.
	var status, assigned string
	require.NoError(t, conn.QueryRow(ctx,
		`SELECT status, assigned_resource_id::text FROM slots WHERE id = $1`, demoActive).Scan(&status, &assigned))
	assert.Equal(t, "ACTIVE", status)
	assert.Equal(t, demoResA, assigned)

	// The second slot is SCHEDULED and still unplaced (no resource yet).
	var pulledStatus string
	var pulledResource *string
	require.NoError(t, conn.QueryRow(ctx,
		`SELECT status, assigned_resource_id::text FROM slots WHERE id = $1`, demoPulled).Scan(&pulledStatus, &pulledResource))
	assert.Equal(t, "SCHEDULED", pulledStatus)
	assert.Nil(t, pulledResource, "unplaced slot has no assigned resource")
}
