//go:build database

package schematest

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SQLSTATE codes asserted by the constraint/trigger tests, named for readability.
const (
	codeNotNullViolation    = "23502"
	codeForeignKeyViolation = "23503"
	codeUniqueViolation     = "23505"
	codeCheckViolation      = "23514"
	codeExclusionViolation  = "23P01"
)

// connect opens a connection to the throwaway schema-test database. It skips the
// test when SCHEMA_TEST_DATABASE_URL is unset so a stray `go test -tags database`
// outside `mage testSchema` is a skip, not a failure.
func connect(t *testing.T) *pgx.Conn {
	t.Helper()
	url := os.Getenv("SCHEMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SCHEMA_TEST_DATABASE_URL not set; run via `mage testSchema`")
	}
	conn, err := pgx.Connect(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// fixture is an isolated lab + member + pool + two resources, created inside a
// transaction that withFixture rolls back, so cases never collide with each other
// or the seed.
type fixture struct {
	labID, userID, poolID, res1ID, res2ID string
}

// withFixture builds the fixture in a transaction, runs fn against it, then rolls
// back unconditionally. IDs come back as text so they scan into plain strings.
func withFixture(t *testing.T, conn *pgx.Conn, fn func(ctx context.Context, tx pgx.Tx, f fixture)) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	var f fixture
	require.NoError(t, tx.QueryRow(ctx,
		`INSERT INTO labs (name) VALUES ('fixture lab') RETURNING id::text`).Scan(&f.labID))
	require.NoError(t, tx.QueryRow(ctx,
		`INSERT INTO users (email) VALUES (lower(gen_random_uuid()::text) || '@example.com')
		 RETURNING id::text`).Scan(&f.userID))
	_, err = tx.Exec(ctx,
		`INSERT INTO lab_memberships (lab_id, user_id, role) VALUES ($1, $2, 'MEMBER')`,
		f.labID, f.userID)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRow(ctx,
		`INSERT INTO resource_pools (lab_id, kind, name) VALUES ($1, 'VENT_HOOD', 'pool')
		 RETURNING id::text`, f.labID).Scan(&f.poolID))
	require.NoError(t, tx.QueryRow(ctx,
		`INSERT INTO resources (resource_pool_id, lab_id, kind, name)
		 VALUES ($1, $2, 'VENT_HOOD', 'A') RETURNING id::text`, f.poolID, f.labID).Scan(&f.res1ID))
	require.NoError(t, tx.QueryRow(ctx,
		`INSERT INTO resources (resource_pool_id, lab_id, kind, name)
		 VALUES ($1, $2, 'VENT_HOOD', 'B') RETURNING id::text`, f.poolID, f.labID).Scan(&f.res2ID))

	fn(ctx, tx, f)
}

// requirePgCode asserts err is a Postgres error with the given SQLSTATE.
func requirePgCode(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, code, pgErr.Code, "unexpected SQLSTATE; message: %s", pgErr.Message)
}
