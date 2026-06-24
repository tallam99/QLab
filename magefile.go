//go:build mage

// Mage targets for the local dev stack. Run `mage` with no args to list targets.
//
// Targets shell out to docker compose, goose, buf, go, and python; they use the
// standard library only so the magefile adds no module dependencies.
package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	composeFile = "docker-compose.yml"
	// postgresService is the Postgres service name in docker-compose.yml.
	postgresService = "postgres"
	envFile         = ".env.json"
	envExampleFile  = ".env.example.json"
	migrationsDir   = "backend/migrations"
	seedFile        = "backend/seed/seed.sql"
	// protoDir is the buf module root; buf runs from here so buf.gen.yaml's
	// relative output paths and the npm-pinned TS plugin resolve.
	protoDir      = "proto"
	bufConfigFile = "proto/buf.gen.yaml"
	// frontendDir is the Vite/React PWA root (its own npm package); `mage frontend`
	// runs the frontend gate there.
	frontendDir = "frontend"
	// schemaTestDir holds the DB-level schema tests (constraints/triggers/seed),
	// tagged `database`; schemaTestDB is the throwaway database mage testSchema
	// creates, migrates, and drops so the tests never touch dev data.
	schemaTestDir = "./backend/schema_test/..."
	schemaTestDB  = "qlab_schema_test"
	// integrationTestDir holds the full-stack integration suite (real server + DB +
	// engine over Connect), tagged `integration`; integrationTestDB is the throwaway
	// database its TestMain creates, migrates, and drops.
	integrationTestDir = "./backend/integration_test/..."
	integrationTestDB  = "qlab_integration_test"
	// firebaseProject is the demo project id the Auth emulator runs under. The
	// `demo-` prefix tells the SDK/emulator it is offline (no real credentials).
	firebaseProject = "demo-qlab"
	// firebaseEmulatorHost is where the integration suite (and the local api
	// container) reach the Auth emulator. Locally the compose service publishes this
	// host port; in CI a background emulator binds it — mirroring how Postgres is a
	// compose service locally and a service container in CI.
	firebaseEmulatorHost = "localhost:9099"
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

// runIn is run with the working directory set to dir.
func runIn(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
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

// Migrate applies goose migrations. Against local Postgres by default; CI/CD sets
// MIGRATE_DATABASE_URL to point at a remote database (the deploy pipeline fills it
// from the migrator secret, running migrations before the new revision deploys).
// That override is for trusted automation only — never run migrations against
// staging/prod from a laptop (see docs/deploy.md).
func Migrate() error {
	url := os.Getenv("MIGRATE_DATABASE_URL")
	if url == "" {
		env, err := loadEnv()
		if err != nil {
			return err
		}
		url = env.hostDatabaseURL()
	}
	sqlFiles, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil {
		return err
	}
	if len(sqlFiles) == 0 {
		fmt.Println("migrate: no migrations to apply")
		return nil
	}
	return run("go", "run", goosePackage, "-dir", migrationsDir, "postgres", url, "up")
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

// Test runs every test tier: unit, security, and the DB schema tests. TestSchema
// needs a reachable Postgres, so `mage test` now requires the local stack up
// (mage startStack); in CI each tier runs as its own job with a Postgres service.
func Test() error {
	if err := TestUnit(); err != nil {
		return err
	}
	if err := TestSecurity(); err != nil {
		return err
	}
	if err := TestSchema(); err != nil {
		return err
	}
	return TestIntegration()
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
// against a throwaway database. The suite's TestMain owns that database — it
// creates it fresh, applies all migrations with goose, loads the demo seed, runs,
// and drops it — so this target just hands it the coordinates. It works the same
// locally and in CI; both only need a reachable Postgres (mage startStack locally,
// a Postgres service in CI). Part of `mage test`.
func TestSchema() error {
	env, err := loadEnv()
	if err != nil {
		return err
	}
	abs := func(p string) string {
		if a, err := filepath.Abs(p); err == nil {
			return a
		}
		return p
	}
	testEnv := append(os.Environ(),
		"SCHEMA_TEST_DATABASE_URL="+env.hostDatabaseURLFor(schemaTestDB),
		"SCHEMA_TEST_MIGRATIONS_DIR="+abs(migrationsDir),
		"SCHEMA_TEST_SEED_FILE="+abs(seedFile),
		"SCHEMA_TEST_GOOSE_PKG="+goosePackage,
	)
	// -count=1 disables the test cache: the result depends on live DB state.
	return runWithEnv(testEnv, "go", "test", "-tags", "database", "-count=1", schemaTestDir)
}

// TestIntegration runs the full-stack integration suite: it boots the real server
// (connecting as a non-privileged RLS-bound app role) against a throwaway database
// and drives it through the generated Connect client. The suite's TestMain owns
// that database — creating it fresh, applying all migrations with goose, creating
// the app role, running, and dropping it — so this target just hands it the
// coordinates. It needs a reachable Postgres (mage startStack locally, a Postgres
// service in CI). Part of `mage test`.
func TestIntegration() error {
	env, err := loadEnv()
	if err != nil {
		return err
	}
	abs := func(p string) string {
		if a, err := filepath.Abs(p); err == nil {
			return a
		}
		return p
	}
	testEnv := append(os.Environ(),
		"INTEGRATION_TEST_DATABASE_URL="+env.hostDatabaseURLFor(integrationTestDB),
		"INTEGRATION_TEST_MIGRATIONS_DIR="+abs(migrationsDir),
		"INTEGRATION_TEST_GOOSE_PKG="+goosePackage,
		// Firebase Auth emulator coordinates. FIREBASE_AUTH_EMULATOR_HOST is read by
		// the Admin SDK itself (so token verification targets the emulator); the suite
		// also reads both to build its client and mint test tokens.
		"FIREBASE_PROJECT_ID="+firebaseProject,
		"FIREBASE_AUTH_EMULATOR_HOST="+firebaseEmulatorHost,
	)
	// -count=1 disables the test cache: the result depends on live DB state.
	return runWithEnv(testEnv, "go", "test", "-tags", "integration", "-count=1", integrationTestDir)
}

// mutateDirs are the directories `mage mutate` runs mutation testing over, in
// order. Add logic-dense packages here as they land; glue/infra packages (DB
// wiring, HTTP lifecycle) aren't good mutation fodder.
var mutateDirs = []string{
	// The pure scheduling engine — the product's core logic. The parent holds the
	// domain types + Input.Validate; basic is the reschedule implementation (its two
	// surviving mutants are the documented equivalents: a `<`/`<=` on the
	// unique-by-Validate priority sort and a slice capacity hint — efficacy is not gated).
	"./backend/internal/dynamicqueue",
	"./backend/internal/dynamicqueue/basic",
	// The authentication provisioning state machine: branchy logic with unit tests.
	"./backend/internal/services/authentication/v1",
	// The membership-policy gate every RPC runs behind.
	"./backend/internal/services/authz/v1",
	// Config cross-field validation + the QLAB_ENV decoder.
	"./backend/internal/config",
	// The store-domain → wire conversions shared by both transports.
	"./backend/internal/protoconv",
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

// GenMocks regenerates the testify mocks from .mockery.yaml. Mocks are generated
// on demand rather than committed, so run this before building or testing code
// that imports a mock (then `go mod tidy` to pull the mock runtime deps). Every
// mock file is named <interface>_mock.go.
func GenMocks() error {
	return run("go", "tool", "mockery")
}

// ClearMocks removes generated mock files (every *_mock.go under backend). Mocks
// aren't committed, so this just tidies the working tree.
func ClearMocks() error {
	return filepath.WalkDir("backend", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), "_mock.go") {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("remove %s: %w", path, err)
			}
		}
		return nil
	})
}

// GenSqlc regenerates the type-safe Go store queries from queries.sql against the
// migration schema (see sqlc.yaml). Committed like the other generated code; CI
// checks it is not stale. Run after changing queries.sql or the slots/outbox
// schema.
func GenSqlc() error {
	return run("go", "tool", "sqlc", "generate")
}

// GenProto regenerates Go + TS from the .proto contract via buf, run from the
// proto/ module dir so the buf.gen templates' relative output paths and the
// npm-pinned TS plugin resolve. The Go plugins are the module's pinned `go tool`
// binaries.
//
// Go and TS are generated in SEPARATE invocations because they need opposite
// import handling:
//   - Go (buf.gen.yaml, no --include-imports): imported modules are NOT vendored.
//     buf/validate resolves to the protovalidate BSR Go module — the same one the
//     runtime uses, so it is registered once; vendoring a second copy would panic
//     at init. The well-known types resolve to timestamppb.
//   - TS (buf.gen.ts.yaml, --include-imports): protobuf-es ships no buf/validate
//     package, so the generated qlab/* TS imports it by relative path; that file
//     must be vendored locally. --include-imports emits buf/validate/validate_pb.ts
//     while still mapping the well-known types to @bufbuild/protobuf/wkt (they are
//     special-cased, not vendored). The frontend never runs protovalidate, so the
//     local descriptor is harmless there.
func GenProto() error {
	if _, err := os.Stat(bufConfigFile); err != nil {
		fmt.Printf("genproto: no buf config (%s) found\n", bufConfigFile)
		return nil
	}
	if err := runIn(protoDir, "buf", "generate"); err != nil {
		return err
	}
	return runIn(protoDir, "buf", "generate", "--template", "buf.gen.ts.yaml", "--include-imports")
}

// Frontend runs the frontend gate — a clean install then lint (Biome), unit tests
// (Vitest), and a production build — the same set the CI `frontend` job enforces.
// Mirrors the npm scripts in frontend/package.json so a green run means the same
// thing locally and in CI. Uses `npm ci` for a reproducible, lockfile-pinned
// install (it wipes and reinstalls node_modules).
//
// `build` already type-checks (`tsc --noEmit && vite build`), so the standalone
// `typecheck` script is not run here — that would type-check the whole project
// twice per gate for no added coverage. `npm run typecheck` stays for fast local
// feedback without a bundle.
func Frontend() error {
	if err := runIn(frontendDir, "npm", "ci"); err != nil {
		return err
	}
	for _, script := range []string{"lint", "test", "build"} {
		if err := runIn(frontendDir, "npm", "run", script); err != nil {
			return err
		}
	}
	return nil
}
