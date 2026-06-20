//go:build mage

// Mage targets for the local dev stack — the contract for "Claude operates local
// infra" (see docs/runbook.md). Run `mage` with no args to list targets.
//
// Targets shell out to docker compose, goose, buf, go, and python; they use the
// standard library only so the root module needs no dependencies.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	composeFile    = "docker-compose.yml"
	envFile        = ".env"
	envExampleFile = ".env.example"
	migrationsDir  = "migrations" // relative to backend/ (where the goose tool lives)
	bufConfigFile  = "proto/buf.gen.yaml"
)

// run executes a command from the repo root, streaming stdio through.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// compose runs `docker compose` against the project's compose file.
func compose(args ...string) error {
	return run("docker", append([]string{"compose", "-f", composeFile}, args...)...)
}

// ensureEnv copies .env.example to .env on first run so a clean checkout is one
// command away from a working stack.
func ensureEnv() error {
	if _, err := os.Stat(envFile); err == nil {
		return nil
	}
	data, err := os.ReadFile(envExampleFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", envExampleFile, err)
	}
	if err := os.WriteFile(envFile, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", envFile, err)
	}
	fmt.Printf("created %s from %s\n", envFile, envExampleFile)
	return nil
}

// envValue reads a single KEY's value from .env (simple KEY=VALUE lines).
func envValue(key string) (string, error) {
	f, err := os.Open(envFile)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", envFile, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(name) == key {
			return strings.Trim(strings.TrimSpace(value), `"'`), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s not set in %s", key, envFile)
}

// Up builds and starts the API + Postgres in the background, waiting for Postgres
// to be healthy before the API starts.
func Up() error {
	if err := ensureEnv(); err != nil {
		return err
	}
	return compose("up", "--build", "-d")
}

// Down stops and removes the stack, keeping the Postgres data volume.
func Down() error {
	return compose("down")
}

// Reset wipes everything (including the Postgres volume) and brings the stack
// back up fresh.
func Reset() error {
	if err := compose("down", "-v"); err != nil {
		return err
	}
	return Up()
}

// Migrate applies goose migrations to local Postgres. Migrations live in
// backend/migrations (added in Phase 4). goose errors on an empty directory, so
// until the first migration exists this skips cleanly.
func Migrate() error {
	if err := ensureEnv(); err != nil {
		return err
	}
	sqlFiles, err := filepath.Glob(filepath.Join("backend", migrationsDir, "*.sql"))
	if err != nil {
		return err
	}
	if len(sqlFiles) == 0 {
		fmt.Println("migrate: no migrations yet (added with the schema in Phase 4)")
		return nil
	}
	url, err := envValue("DATABASE_URL")
	if err != nil {
		return err
	}
	// goose is pinned as a tool dependency in backend/go.mod; run it from there.
	return run("go", "-C", "backend", "tool", "goose", "-dir", migrationsDir, "postgres", url, "up")
}

// Seed loads demo data into local Postgres. The seed lands with the schema in
// Phase 4; this placeholder keeps the target in the contract until then.
func Seed() error {
	fmt.Println("seed: no seed data yet (lands with the schema in Phase 4)")
	return nil
}

// Test runs the full local suite: the Go tests and the Yaak secret-check tests,
// so standalone tooling isn't orphaned. CI runs the same set.
func Test() error {
	if err := run("go", "-C", "backend", "test", "./..."); err != nil {
		return err
	}
	return run("python3", "scripts/test_check_yaak_secrets.py")
}

// Logs follows the stack's logs (last 100 lines, then live).
func Logs() error {
	return compose("logs", "-f", "--tail=100")
}

// Proto regenerates Go + TS from the .proto contract via buf. The contract and
// buf config land in Phase 5; until then this reports there's nothing to do.
func Proto() error {
	if _, err := os.Stat(bufConfigFile); err != nil {
		fmt.Printf("proto: no buf config yet (%s lands in Phase 5)\n", bufConfigFile)
		return nil
	}
	return run("buf", "generate")
}
