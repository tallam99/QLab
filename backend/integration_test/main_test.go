//go:build integration

// Package integrationtest exercises the full Phase-7 stack front to back: a real
// generated Connect client → the real HTTP server (router, middleware, auth seam,
// Connect handlers) → the scheduling service → the pure engine → a real Postgres.
//
// It is deliberately faithful to production wiring:
//   - the server connects as a non-privileged, RLS-bound app role (a stand-in for
//     the cloud qlab_app role), so tenant isolation is genuinely enforced;
//   - the harness arranges state through a separate superuser connection that
//     bypasses RLS (the "admin" pool), then drives behaviour only through the API;
//   - time is injected via a settable clock, so overrun / grace / pull-forward are
//     deterministic.
//
// TestMain owns the throwaway database lifecycle (create, migrate, grant, drop) and
// the running server, mirroring backend/schema_test so the flow is identical
// locally and in CI — both only need a reachable Postgres. Run via
// `mage testIntegration`, which injects the coordinates below.
package integrationtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/tallam99/qlab/backend/internal/dynamicqueue"
)

// Coordinates injected by `mage testIntegration` (the only supported way to run
// these). Required, not optional: reaching here without them is a mistake, so we
// fail loudly rather than skip and report a false green.
var (
	adminDatabaseURL string // superuser URL to the throwaway DB (arranges state, bypasses RLS)
	migrationsDir    string // absolute path to backend/migrations
	goosePackage     string // go-run target for goose (pinned version)
)

// The non-privileged role the server connects as — a stand-in for the cloud
// qlab_app role: NOSUPERUSER, NOBYPASSRLS, so row-level security actually applies.
const (
	appRole     = "qlab_app_itest"
	appPassword = "integration_app"
)

// testGrace is the clock-in grace the engine is configured with for the suite.
const testGrace = dynamicqueue.Minutes(15)

// h is the shared harness; every test uses it. Tests run serially (they share one
// database and one clock), so they must reset state with h.reset between them.
var h *harness

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration setup failed (is Postgres up? `mage startStack`):", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// run wraps the lifecycle so defers (drop DB, shut down the server) fire before
// os.Exit.
func run(m *testing.M) (int, error) {
	adminDatabaseURL = mustEnv("INTEGRATION_TEST_DATABASE_URL")
	migrationsDir = mustEnv("INTEGRATION_TEST_MIGRATIONS_DIR")
	goosePackage = mustEnv("INTEGRATION_TEST_GOOSE_PKG")

	if err := setupDatabase(); err != nil {
		return 1, err
	}
	defer func() {
		if err := dropDatabase(); err != nil {
			fmt.Fprintln(os.Stderr, "integration teardown warning:", err)
		}
	}()

	appURL, err := withCredentials(adminDatabaseURL, appRole, appPassword)
	if err != nil {
		return 1, err
	}

	started, err := startHarness(appURL)
	if err != nil {
		return 1, err
	}
	defer started.shutdown()
	h = started

	return m.Run(), nil
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		fmt.Fprintf(os.Stderr, "%s is required; run via `mage testIntegration`\n", name)
		os.Exit(1)
	}
	return v
}

// adminConfig parses the throwaway DB URL and points a copy at the maintenance
// database so we can CREATE/DROP the test database itself.
func adminConfig() (*pgx.ConnConfig, string, error) {
	cfg, err := pgx.ParseConfig(adminDatabaseURL)
	if err != nil {
		return nil, "", fmt.Errorf("parse INTEGRATION_TEST_DATABASE_URL: %w", err)
	}
	name := cfg.Database
	admin := cfg.Copy()
	admin.Database = "postgres" // always present on a local/CI server
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
	roleIdent := pgx.Identifier{appRole}.Sanitize()
	// Drop the DB first (removes its grants) so the global role has no leftover
	// dependencies, then recreate both fresh.
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)"); err != nil {
		return fmt.Errorf("drop test database: %w", err)
	}
	if _, err := conn.Exec(ctx, "DROP ROLE IF EXISTS "+roleIdent); err != nil {
		return fmt.Errorf("drop app role: %w", err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(
		"CREATE ROLE %s LOGIN PASSWORD %s NOSUPERUSER NOBYPASSRLS",
		roleIdent, quoteLiteral(appPassword))); err != nil {
		return fmt.Errorf("create app role: %w", err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		return fmt.Errorf("create test database: %w", err)
	}

	// Apply migrations with the real goose, so the suite exercises the exact schema
	// production gets.
	migrate := exec.Command("go", "run", goosePackage, "-dir", migrationsDir, "postgres", adminDatabaseURL, "up")
	migrate.Stdout, migrate.Stderr = os.Stderr, os.Stderr
	if err := migrate.Run(); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}

	// Grant the app role the same DML it gets in cloud (decision 0004). Runs as the
	// superuser owner, which bypasses RLS.
	owner, err := pgx.Connect(ctx, adminDatabaseURL)
	if err != nil {
		return fmt.Errorf("connect for grants: %w", err)
	}
	defer owner.Close(ctx)
	grants := fmt.Sprintf(`
		GRANT USAGE ON SCHEMA public TO %[1]s;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %[1]s;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %[1]s;`, roleIdent)
	if _, err := owner.Exec(ctx, grants); err != nil {
		return fmt.Errorf("grant to app role: %w", err)
	}
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
	_, err = conn.Exec(ctx, "DROP ROLE IF EXISTS "+pgx.Identifier{appRole}.Sanitize())
	return err
}

// withCredentials returns the URL with its userinfo replaced — used to derive the
// app-role connection string from the superuser one.
func withCredentials(rawURL, user, password string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse database url: %w", err)
	}
	u.User = url.UserPassword(user, password)
	return u.String(), nil
}

// quoteLiteral wraps a string as a SQL string literal (for DDL that can't take a
// bind parameter, e.g. CREATE ROLE ... PASSWORD).
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
