//go:build database

package schematest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// demoMember is a seeded MEMBER of the demo lab (see backend/seed/seed.sql).
const demoMember = "20000000-0000-0000-0000-000000000002"

// appScalar runs a count/scalar query on an app-role connection.
func appScalar(t *testing.T, ctx context.Context, conn *pgx.Conn, query string) int {
	t.Helper()
	var n int
	require.NoError(t, conn.QueryRow(ctx, query).Scan(&n))
	return n
}

// setLab sets the session's tenant context (the GUC the RLS policies read).
func setLab(t *testing.T, ctx context.Context, conn *pgx.Conn, lab string) {
	t.Helper()
	_, err := conn.Exec(ctx, "SELECT set_config('app.current_lab_id', $1, false)", lab)
	require.NoError(t, err)
}

// TestRLSReadIsolation checks that the non-privileged app role only sees rows for
// the lab in its session context, and nothing at all without one (fail-closed).
// Same role, same data — visibility depends solely on app.current_lab_id.
func TestRLSReadIsolation(t *testing.T) {
	ctx := context.Background()

	t.Run("scoped to the demo lab sees its rows", func(t *testing.T) {
		conn := connectAsApp(t)
		setLab(t, ctx, conn, demoLab)
		assert.Equal(t, 1, appScalar(t, ctx, conn, "SELECT count(*) FROM labs"))
		assert.Equal(t, 5, appScalar(t, ctx, conn, "SELECT count(*) FROM slots"))
		assert.Equal(t, 5, appScalar(t, ctx, conn, "SELECT count(*) FROM labs_users"))
	})

	t.Run("scoped to another lab sees nothing", func(t *testing.T) {
		conn := connectAsApp(t)
		setLab(t, ctx, conn, "00000000-0000-0000-0000-0000000000ff")
		assert.Equal(t, 0, appScalar(t, ctx, conn, "SELECT count(*) FROM labs"))
		assert.Equal(t, 0, appScalar(t, ctx, conn, "SELECT count(*) FROM slots"))
	})

	t.Run("no lab context sees nothing (fail-closed)", func(t *testing.T) {
		conn := connectAsApp(t)
		assert.Equal(t, 0, appScalar(t, ctx, conn, "SELECT count(*) FROM slots"))
		assert.Equal(t, 0, appScalar(t, ctx, conn, "SELECT count(*) FROM labs"))
	})
}

// TestRLSWriteRespectsTenant checks the WITH CHECK side: the app role can write
// into its scoped lab, but a write with no/!matching context is refused.
func TestRLSWriteRespectsTenant(t *testing.T) {
	ctx := context.Background()

	t.Run("insert into the scoped lab succeeds", func(t *testing.T) {
		conn := connectAsApp(t)
		tx, err := conn.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, "SELECT set_config('app.current_lab_id', $1, true)", demoLab)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `INSERT INTO slots
			(labs_id, users_id, resource_pools_id, slot_priority, desired_start, lookahead, duration)
			VALUES ($1, $2, $3, 99, now(), 0, 30)`, demoLab, demoMember, demoPool)
		require.NoError(t, err)
	})

	t.Run("insert with no lab context is refused (fail-closed)", func(t *testing.T) {
		conn := connectAsApp(t)
		_, err := conn.Exec(ctx, `INSERT INTO slots
			(labs_id, users_id, resource_pools_id, slot_priority, desired_start, lookahead, duration)
			VALUES ($1, $2, $3, 98, now(), 0, 30)`, demoLab, demoMember, demoPool)
		requirePgCode(t, err, codeInsufficientPrivilege)
	})
}
