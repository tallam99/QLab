//go:build database

package schematest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SQLSTATE codes asserted by the constraint/trigger tests, named for readability.
const (
	codeForeignKeyViolation       = "23503"
	codeUniqueViolation           = "23505"
	codeCheckViolation            = "23514"
	codeExclusionViolation        = "23P01"
	codeInsufficientPrivilege     = "42501" // raised by an RLS policy violation
	codeInvalidTextRepresentation = "22P02" // e.g. casting a non-UUID string to uuid
)

// Coordinates injected by `mage testSchema` (the only supported way to run these).
// They are required, not optional: a run that reaches here without them is a
// mistake, so we fail loudly rather than skip and report a false green.
var (
	testDatabaseURL string // the throwaway DB this suite owns
	migrationsDir   string // absolute path to backend/migrations
	seedFile        string // absolute path to backend/seed/seed.sql
	goosePackage    string // go-run target for goose (pinned version)
)

// The non-privileged role the RLS tests connect as — a stand-in for the cloud
// qlab_app role: a plain LOGIN role, NOSUPERUSER and NOBYPASSRLS, so row-level
// security actually applies to it (superusers and the table owner bypass RLS).
// TestMain creates it, grants it the same DML the app gets, and builds its config.
const (
	testAppRole     = "qlab_app_test"
	testAppPassword = "schema_test_app"
)

var appRoleConfig *pgx.ConnConfig // connection config for testAppRole; set in setup

// TestMain owns the throwaway database lifecycle: it creates a fresh DB, applies
// every migration with the real goose, loads the demo seed, runs the suite, and
// drops the DB. Doing it here (rather than in the magefile) keeps the flow
// identical locally and in CI — both just need a reachable Postgres.
func TestMain(m *testing.M) {
	testDatabaseURL = mustEnv("SCHEMA_TEST_DATABASE_URL")
	migrationsDir = mustEnv("SCHEMA_TEST_MIGRATIONS_DIR")
	seedFile = mustEnv("SCHEMA_TEST_SEED_FILE")
	goosePackage = mustEnv("SCHEMA_TEST_GOOSE_PKG")

	if err := setupDatabase(); err != nil {
		fmt.Fprintln(os.Stderr, "schema-test setup failed (is Postgres up? `mage startStack`):", err)
		os.Exit(1)
	}
	code := m.Run()
	if err := dropDatabase(); err != nil {
		fmt.Fprintln(os.Stderr, "schema-test teardown warning:", err)
	}
	os.Exit(code)
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		fmt.Fprintf(os.Stderr, "%s is required; run the schema tests via `mage testSchema`\n", name)
		os.Exit(1)
	}
	return v
}

// adminConfig parses the test DB URL and points it at the maintenance database so
// we can CREATE/DROP the test database itself. Returns the config and the test
// database's name.
func adminConfig() (*pgx.ConnConfig, string, error) {
	cfg, err := pgx.ParseConfig(testDatabaseURL)
	if err != nil {
		return nil, "", fmt.Errorf("parse SCHEMA_TEST_DATABASE_URL: %w", err)
	}
	name := cfg.Database
	admin := cfg.Copy()
	admin.Database = "postgres" // always present on a local/CI Postgres server
	return admin, name, nil
}

func setupDatabase() error {
	ctx := context.Background()
	admin, name, err := adminConfig()
	if err != nil {
		return err
	}
	conn, err := pgx.ConnectConfig(ctx, admin)
	if err != nil {
		return fmt.Errorf("connect admin: %w", err)
	}
	defer conn.Close(ctx)
	ident := pgx.Identifier{name}.Sanitize()
	roleIdent := pgx.Identifier{testAppRole}.Sanitize()
	// Drop the DB first (removes its grants), so dropping/recreating the global role
	// has no leftover dependencies.
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)"); err != nil {
		return fmt.Errorf("drop test database: %w", err)
	}
	if _, err := conn.Exec(ctx, "DROP ROLE IF EXISTS "+roleIdent); err != nil {
		return fmt.Errorf("drop test role: %w", err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(
		"CREATE ROLE %s LOGIN PASSWORD %s NOSUPERUSER NOBYPASSRLS",
		roleIdent, quoteLiteral(testAppPassword))); err != nil {
		return fmt.Errorf("create test role: %w", err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		return fmt.Errorf("create test database: %w", err)
	}

	// Apply migrations with the real goose, so the tests exercise the exact schema
	// production gets.
	migrate := exec.Command("go", "run", goosePackage, "-dir", migrationsDir, "postgres", testDatabaseURL, "up")
	migrate.Stdout, migrate.Stderr = os.Stderr, os.Stderr
	if err := migrate.Run(); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}

	// Grant the app role the same DML it gets in cloud (decision 0004), then seed.
	// Both run as the superuser owner, which bypasses RLS.
	seedConn, err := pgx.Connect(ctx, testDatabaseURL)
	if err != nil {
		return fmt.Errorf("connect for grants/seed: %w", err)
	}
	defer seedConn.Close(ctx)
	grants := fmt.Sprintf(`
		GRANT USAGE ON SCHEMA public TO %[1]s;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %[1]s;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %[1]s;`, roleIdent)
	if _, err := seedConn.Exec(ctx, grants); err != nil {
		return fmt.Errorf("grant to app role: %w", err)
	}

	// Load the demo seed (multi-statement script via the simple protocol).
	sql, err := os.ReadFile(seedFile)
	if err != nil {
		return fmt.Errorf("read seed: %w", err)
	}
	if _, err := seedConn.Exec(ctx, string(sql)); err != nil {
		return fmt.Errorf("apply seed: %w", err)
	}

	// Build the app-role connection config (same DB, non-privileged role).
	appCfg, err := pgx.ParseConfig(testDatabaseURL)
	if err != nil {
		return fmt.Errorf("parse app role config: %w", err)
	}
	appCfg.User = testAppRole
	appCfg.Password = testAppPassword
	appRoleConfig = appCfg
	return nil
}

func dropDatabase() error {
	ctx := context.Background()
	admin, name, err := adminConfig()
	if err != nil {
		return err
	}
	conn, err := pgx.ConnectConfig(ctx, admin)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
		return err
	}
	// The DB (and its grants) is gone, so the global role has no dependencies left.
	_, err = conn.Exec(ctx, "DROP ROLE IF EXISTS "+pgx.Identifier{testAppRole}.Sanitize())
	return err
}

// quoteLiteral wraps a string as a SQL string literal (for DDL that can't take a
// bind parameter, e.g. CREATE ROLE ... PASSWORD).
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// connect opens a connection to the throwaway schema-test database as the
// superuser owner (which bypasses RLS). TestMain has already validated the URL, so
// a failure here is a real error, not a skip.
func connect(t *testing.T) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), testDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// connectAsApp connects as the non-privileged app role, so RLS policies apply —
// the connection used by the row-level-security tests.
func connectAsApp(t *testing.T) *pgx.Conn {
	t.Helper()
	conn, err := pgx.ConnectConfig(context.Background(), appRoleConfig.Copy())
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
		`INSERT INTO users (email) VALUES (lower(gen_random_uuid()::text) || '@example.com')
		 RETURNING users_id::text`).Scan(&f.userID))
	require.NoError(t, tx.QueryRow(ctx,
		`INSERT INTO labs (name) VALUES ('fixture lab') RETURNING labs_id::text`).Scan(&f.labID))
	_, err = tx.Exec(ctx,
		`INSERT INTO labs_users (labs_id, users_id, role) VALUES ($1, $2, 'MEMBER')`,
		f.labID, f.userID)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRow(ctx,
		`INSERT INTO resource_pools (labs_id, kind, name) VALUES ($1, 'VENT_HOOD', 'pool')
		 RETURNING resource_pools_id::text`, f.labID).Scan(&f.poolID))
	require.NoError(t, tx.QueryRow(ctx,
		`INSERT INTO resources (resource_pools_id, labs_id, kind, name)
		 VALUES ($1, $2, 'VENT_HOOD', 'A') RETURNING resources_id::text`, f.poolID, f.labID).Scan(&f.res1ID))
	require.NoError(t, tx.QueryRow(ctx,
		`INSERT INTO resources (resource_pools_id, labs_id, kind, name)
		 VALUES ($1, $2, 'VENT_HOOD', 'B') RETURNING resources_id::text`, f.poolID, f.labID).Scan(&f.res2ID))

	fn(ctx, tx, f)
}

// requirePgCode asserts err is a Postgres error with the given SQLSTATE.
func requirePgCode(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, code, pgErr.Code, "unexpected SQLSTATE; message: %s", pgErr.Message)
}
