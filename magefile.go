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
	composeFile    = "docker-compose.yml"
	envFile        = ".env.json"
	envExampleFile = ".env.example.json"
	migrationsDir  = "backend/migrations"
	bufConfigFile  = "proto/buf.gen.yaml"
	// gooseVersion pins the migration tool. It's run via `go run …@version` rather
	// than a go.mod tool dependency so its many DB-driver deps don't bloat the
	// module (we only use Postgres).
	goosePackage = "github.com/pressly/goose/v3/cmd/goose@v3.27.1"
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
func (e Env) hostDatabaseURL() string {
	return fmt.Sprintf("postgres://%s:%s@localhost:%s/%s?sslmode=disable",
		e.PostgresUser, e.PostgresPassword, e.PostgresPort, e.PostgresDB)
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
// backend/migrations (added in Phase 4). goose errors on an empty directory, so
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
		fmt.Println("migrate: no migrations yet (added with the schema in Phase 4)")
		return nil
	}
	return run("go", "run", goosePackage, "-dir", migrationsDir, "postgres", env.hostDatabaseURL(), "up")
}

// Seed loads demo data into local Postgres. The seed lands with the schema in
// Phase 4; this placeholder keeps the target in the contract until then.
func Seed() error {
	fmt.Println("seed: no seed data yet (lands with the schema in Phase 4)")
	return nil
}

// TestUnit runs the unit tests (build tag `testunit`) plus the Yaak secret-check
// tests. Unit tests need no infrastructure; integration/database suites get their
// own tags and targets as they land.
func TestUnit() error {
	if err := run("go", "test", "-tags", "testunit", "./backend/..."); err != nil {
		return err
	}
	return run("python3", "scripts/test_check_yaak_secrets.py")
}

// ServiceLogs follows the running services' logs (last 100 lines, then live) —
// like watching them in Docker Desktop.
func ServiceLogs() error {
	env, err := loadEnv()
	if err != nil {
		return err
	}
	return compose(env, "logs", "-f", "--tail=100")
}

// GenProto regenerates Go + TS from the .proto contract via buf. The contract and
// buf config land in Phase 5; until then this reports there's nothing to do.
func GenProto() error {
	if _, err := os.Stat(bufConfigFile); err != nil {
		fmt.Printf("genproto: no buf config yet (%s lands in Phase 5)\n", bufConfigFile)
		return nil
	}
	return run("buf", "generate")
}
