//go:build mage

// Mage targets for the local dev stack. Run `mage` with no args to list targets.
//
// Targets shell out to docker compose, goose, buf, go, and python; they use the
// standard library only so the magefile adds no module dependencies.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	composeFile = "docker-compose.yml"
	// postgresService is the Postgres service name in docker-compose.yml.
	postgresService = "postgres"
	envFile         = ".env.json"
	envExampleFile  = ".env.example.json"
	migrationsDir   = "backend/migrations"
	seedFile        = "backend/seed/seed.sql"
	bufConfigFile   = "proto/buf.gen.yaml"
	// schemaTestDir holds the DB-level schema tests (constraints/triggers/seed),
	// tagged `database`; schemaTestDB is the throwaway database mage testSchema
	// creates, migrates, and drops so the tests never touch dev data.
	schemaTestDir = "./backend/schema_test/..."
	schemaTestDB  = "qlab_schema_test"
	// engineDir is the pure scheduling engine; mutation testing recurses it.
	engineDir = "./backend/internal/dynamicqueue"
	// goosePackage pins the migration tool. It's run via `go run …@version` rather
	// than a go.mod tool dependency so its many DB-driver deps don't bloat the
	// module (we only use Postgres).
	goosePackage = "github.com/pressly/goose/v3/cmd/goose@v3.27.1"

	// Cloud DB access (dbStringStaging / dbStringProd) — USER-RUN ONLY. These read
	// the human read-write connection string from the matching project's Secret
	// Manager. The service itself connects as a dedicated least-privilege Neon role
	// (its string is the db-url-<env> secret); these *-readwrite secrets are
	// separate human-access credentials, never the Neon admin/owner password. See
	// docs/deploy.md for how the roles + secrets are provisioned.
	gcpProjectStaging = "qlab-staging"
	gcpProjectProd    = "qlab-production"
	dbSecretStagingRW = "db-url-staging-readwrite"
	dbSecretProdRW    = "db-url-production-readwrite"
)

// Env is the local dev configuration, loaded from .env.json (see
// .env.example.json). It is the single source of truth for both compose and the
// host-run tooling.
type Env struct {
	PostgresUser     string `json:"postgres_user"`
	PostgresPassword string `json:"postgres_password"`
	PostgresDB       string `json:"postgres_db"`
	PostgresPort     string `json:"postgres_port"`
}

// loadEnv reads .env.json, creating it from the template on first run so a clean
// checkout is one command from a working stack.
func loadEnv() (Env, error) {
	if _, err := os.Stat(envFile); err != nil {
		data, err := os.ReadFile(envExampleFile)
		if err != nil {
			return Env{}, fmt.Errorf("read %s: %w", envExampleFile, err)
		}
		if err := os.WriteFile(envFile, data, 0o644); err != nil {
			return Env{}, fmt.Errorf("write %s: %w", envFile, err)
		}
		fmt.Printf("created %s from %s\n", envFile, envExampleFile)
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		return Env{}, fmt.Errorf("read %s: %w", envFile, err)
	}
	var e Env
	if err := json.Unmarshal(data, &e); err != nil {
		return Env{}, fmt.Errorf("parse %s: %w", envFile, err)
	}
	return e, nil
}

// composeEnv returns the process environment plus the variables
// docker-compose.yml interpolates. Passing them here (instead of a .env file)
// keeps .env.json the single source of truth.
func (e Env) composeEnv() []string {
	return append(os.Environ(),
		"POSTGRES_USER="+e.PostgresUser,
		"POSTGRES_PASSWORD="+e.PostgresPassword,
		"POSTGRES_DB="+e.PostgresDB,
		"POSTGRES_PORT="+e.PostgresPort,
	)
}

// hostDatabaseURL is the connection string for host-run tooling (goose), which
// reaches Postgres on the published host port.
func (e Env) hostDatabaseURL() string { return e.hostDatabaseURLFor(e.PostgresDB) }

// hostDatabaseURLFor is hostDatabaseURL for an arbitrary database on the same
// local server (e.g. the throwaway schema-test database).
func (e Env) hostDatabaseURLFor(db string) string {
	return fmt.Sprintf("postgres://%s:%s@localhost:%s/%s?sslmode=disable",
		e.PostgresUser, e.PostgresPassword, e.PostgresPort, db)
}

// run executes a command, streaming stdio, inheriting the process environment.
func run(name string, args ...string) error {
	return runWithEnv(nil, name, args...)
}

// runWithEnv is run with an explicit environment (nil inherits the parent's).
func runWithEnv(env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// compose runs `docker compose` with the dev variables in its environment.
func compose(env Env, args ...string) error {
	return runWithEnv(env.composeEnv(), "docker", append([]string{"compose", "-f", composeFile}, args...)...)
}

// psql runs psql inside the Postgres container against database db. stdinPath, if
// non-empty, is opened and fed to psql's stdin (for `-f -`). Running psql in the
// container avoids depending on a host psql install. ON_ERROR_STOP makes any SQL
// error fail the command rather than passing silently.
func psql(env Env, db, stdinPath string, args ...string) error {
	full := append([]string{
		"compose", "-f", composeFile, "exec", "-T", postgresService,
		"psql", "-v", "ON_ERROR_STOP=1", "-U", env.PostgresUser, "-d", db,
	}, args...)
	cmd := exec.Command("docker", full...)
	cmd.Env = env.composeEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if stdinPath != "" {
		f, err := os.Open(stdinPath)
		if err != nil {
			return err
		}
		defer f.Close()
		cmd.Stdin = f
	}
	return cmd.Run()
}

// StartStack builds and starts the API + Postgres in the background, waiting for
// Postgres to be healthy before the API starts.
func StartStack() error {
	env, err := loadEnv()
	if err != nil {
		return err
	}
	return compose(env, "up", "--build", "-d")
}

// StopStack stops and removes the stack, keeping the Postgres data volume.
func StopStack() error {
	env, err := loadEnv()
	if err != nil {
		return err
	}
	return compose(env, "down")
}

// ResetStack wipes everything (including the Postgres volume) and brings the
// stack back up fresh.
func ResetStack() error {
	env, err := loadEnv()
	if err != nil {
		return err
	}
	if err := compose(env, "down", "-v"); err != nil {
		return err
	}
	return StartStack()
}

// Migrate applies goose migrations to local Postgres. Migrations live in
// backend/migrations (added in Phase 5). goose errors on an empty directory, so
// until the first migration exists this skips cleanly.
func Migrate() error {
	env, err := loadEnv()
	if err != nil {
		return err
	}
	sqlFiles, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil {
		return err
	}
	if len(sqlFiles) == 0 {
		fmt.Println("migrate: no migrations yet (added with the schema in Phase 5)")
		return nil
	}
	return run("go", "run", goosePackage, "-dir", migrationsDir, "postgres", env.hostDatabaseURL(), "up")
}

// Seed loads demo data into local Postgres by applying backend/seed/seed.sql. It
// is LOCAL ONLY by construction: it runs psql inside the Compose Postgres
// container, which has no route to Neon staging/prod. Demo data lives only here;
// anything that must exist in staging/prod goes in a migration instead. Requires
// the stack up (mage startStack) and migrations applied (mage migrate).
func Seed() error {
	env, err := loadEnv()
	if err != nil {
		return err
	}
	return psql(env, env.PostgresDB, seedFile, "-f", "-")
}

// Test runs every test tier. Integration and database tiers (testIntegration,
// testDatabase) get their own targets and are added here as they land.
func Test() error {
	if err := TestUnit(); err != nil {
		return err
	}
	return TestSecurity()
}

// TestUnit runs the Go unit tests (build tag `testunit`). They need no
// infrastructure; integration/database suites get their own tags and targets.
func TestUnit() error {
	return run("go", "test", "-tags", "testunit", "./backend/...")
}

// TestSecurity runs the security checks: the Yaak secret-scanner's own tests, and
// the scanner itself against the committed workspace (no real credentials may be
// committed). Mirrors the CI security job and the lefthook pre-commit hook.
func TestSecurity() error {
	if err := run("python3", "scripts/test_check_yaak_secrets.py"); err != nil {
		return err
	}
	return run("python3", "scripts/check-yaak-secrets.py")
}

// TestSchema runs the DB-level schema tests (constraints, triggers, seed values)
// against a throwaway database. It creates qlab_schema_test fresh, applies all
// migrations, loads the demo seed, runs the `database`-tagged Go tests against it,
// and drops it — so it never touches dev data and is repeatable. Requires the
// local stack (mage startStack); it is NOT part of `mage test` because that runs
// in CI without a database. The tests reach the throwaway DB via the
// SCHEMA_TEST_DATABASE_URL it sets.
func TestSchema() error {
	env, err := loadEnv()
	if err != nil {
		return err
	}
	// Recreate the throwaway DB. WITH (FORCE) drops any lingering connections from
	// a previous interrupted run.
	recreate := func() error {
		return psql(env, "postgres", "",
			"-c", "DROP DATABASE IF EXISTS "+schemaTestDB+" WITH (FORCE)",
			"-c", "CREATE DATABASE "+schemaTestDB)
	}
	if err := recreate(); err != nil {
		return fmt.Errorf("create schema-test database (is the stack up? run `mage startStack`): %w", err)
	}
	// Always drop the throwaway DB on the way out, even if the tests fail.
	defer func() {
		_ = psql(env, "postgres", "", "-c", "DROP DATABASE IF EXISTS "+schemaTestDB+" WITH (FORCE)")
	}()

	testURL := env.hostDatabaseURLFor(schemaTestDB)
	if err := run("go", "run", goosePackage, "-dir", migrationsDir, "postgres", testURL, "up"); err != nil {
		return fmt.Errorf("migrate schema-test database: %w", err)
	}
	if err := psql(env, schemaTestDB, seedFile, "-f", "-"); err != nil {
		return fmt.Errorf("seed schema-test database: %w", err)
	}
	return runWithEnv(append(os.Environ(), "SCHEMA_TEST_DATABASE_URL="+testURL),
		"go", "test", "-tags", "database", schemaTestDir)
}

// mutateDirs are the directories `mage mutate` runs mutation testing over, in
// order. Add logic-dense packages here as they land; glue/infra packages (DB
// wiring, HTTP lifecycle) aren't good mutation fodder.
var mutateDirs = []string{
	engineDir,
}

// Mutate runs mutation testing (gremlins) over mutateDirs to verify the test suite
// actually kills injected faults, not just executes them. Settings (build tag,
// timeout, excluded generated files, thresholds) come from .gremlins.yaml, which
// CI shares. It gates on mutant coverage, so it exits non-zero if any reachable
// mutant goes unexercised. Not part of `mage test`. Needs gremlins on PATH —
// install with `brew install go-gremlins/tap/gremlins`.
func Mutate() error {
	if _, err := exec.LookPath("gremlins"); err != nil {
		return fmt.Errorf("gremlins not found on PATH; install with `brew install go-gremlins/tap/gremlins`: %w", err)
	}
	for _, dir := range mutateDirs {
		if err := run("gremlins", "unleash", dir); err != nil {
			return fmt.Errorf("mutate %s: %w", dir, err)
		}
	}
	return nil
}

// ServiceLogs follows all services' logs (last 100 lines, then live).
func ServiceLogs() error {
	env, err := loadEnv()
	if err != nil {
		return err
	}
	return compose(env, "logs", "-f", "--tail=100")
}

// PostgresLogs dumps the Postgres container's full log, then streams it live —
// useful for debugging database startup.
func PostgresLogs() error {
	env, err := loadEnv()
	if err != nil {
		return err
	}
	return compose(env, "logs", "-f", postgresService)
}

// DbStringStaging prints the STAGING human read-write Postgres connection string,
// read from Secret Manager using your logged-in gcloud identity. Paste it into
// DBeaver (or psql). It is NOT the Neon admin password and NOT the service's
// credential — it is a dedicated human read-write role (see docs/deploy.md).
//
// USER-RUN ONLY. Per the project boundary (CLAUDE.md) Claude never authenticates
// to or invokes gcloud — do not run this target as Claude; it is here for the user.
func DbStringStaging() error { return printDBString(gcpProjectStaging, dbSecretStagingRW) }

// DbStringProd prints the PRODUCTION human read-write connection string. Same
// boundary as DbStringStaging — USER-RUN ONLY; Claude never invokes it.
func DbStringProd() error { return printDBString(gcpProjectProd, dbSecretProdRW) }

// printDBString fetches a secret's latest version from the given GCP project and
// writes it to stdout (no trailing newline from gcloud), ready to paste into a DB
// client. Uses the caller's existing gcloud auth.
func printDBString(project, secret string) error {
	return run("gcloud", "secrets", "versions", "access", "latest",
		"--secret", secret, "--project", project)
}

// GenProto regenerates Go + TS from the .proto contract via buf. The contract and
// buf config land in Phase 6; until then this reports there's nothing to do.
func GenProto() error {
	if _, err := os.Stat(bufConfigFile); err != nil {
		fmt.Printf("genproto: no buf config yet (%s lands in Phase 6)\n", bufConfigFile)
		return nil
	}
	return run("buf", "generate")
}
