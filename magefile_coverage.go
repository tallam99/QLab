//go:build mage

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	// coverPkg credits coverage to every backend package, so the integration tier (which
	// only runs the integration_test package) still counts toward the real code it drives.
	coverPkg = "github.com/tallam99/qlab/backend/..."
	// coverProfile / coverSummary match the gitignored `coverage.*` glob. coverSummary is
	// the markdown CI posts as a PR comment; coverMarker keys the sticky comment.
	coverProfile = "coverage.txt"
	coverSummary = "coverage.md"
	coverMarker  = "<!-- qlab-coverage -->"
	// coverFloor is the per-package health line we flag against (reporting only; the CI
	// job comments, it does not gate — decision recorded with the job).
	coverFloor = 70.0
)

// Coverage runs every Go test tier (unit, schema, integration) with statement coverage
// credited to the backend packages, merges the profiles with `go tool covdata`, and
// writes a per-package + total summary — excluding generated code — to coverage.md.
// That file is the artifact CI posts as a PR comment. Like `mage test`, the schema and
// integration tiers need a reachable Postgres (and the Auth emulator), so run it with
// the local stack up (`mage startStack`).
func Coverage() error {
	env, err := loadEnv()
	if err != nil {
		return err
	}

	covRoot, err := os.MkdirTemp("", "qlab-cov-")
	if err != nil {
		return fmt.Errorf("make coverage dir: %w", err)
	}
	defer os.RemoveAll(covRoot)
	unitDir := filepath.Join(covRoot, "unit")
	schemaDir := filepath.Join(covRoot, "schema")
	integDir := filepath.Join(covRoot, "integ")
	mergedDir := filepath.Join(covRoot, "merged")
	for _, d := range []string{unitDir, schemaDir, integDir, mergedDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	abs := func(p string) string {
		if a, err := filepath.Abs(p); err == nil {
			return a
		}
		return p
	}

	// Each tier writes binary coverage into its own dir via -test.gocoverdir; covdata
	// merges them. -coverpkg is the same set for all three so the merge lines up.
	if err := runWithEnv(os.Environ(), "go", "test", "-tags", "testunit",
		"-cover", "-coverpkg", coverPkg, "./backend/...",
		"-args", "-test.gocoverdir="+unitDir); err != nil {
		return fmt.Errorf("unit coverage: %w", err)
	}

	schemaEnv := append(os.Environ(),
		"SCHEMA_TEST_DATABASE_URL="+env.hostDatabaseURLFor(schemaTestDB),
		"SCHEMA_TEST_MIGRATIONS_DIR="+abs(migrationsDir),
		"SCHEMA_TEST_SEED_FILE="+abs(seedFile),
		"SCHEMA_TEST_GOOSE_PKG="+goosePackage,
	)
	if err := runWithEnv(schemaEnv, "go", "test", "-tags", "database", "-count=1",
		"-cover", "-coverpkg", coverPkg, schemaTestDir,
		"-args", "-test.gocoverdir="+schemaDir); err != nil {
		return fmt.Errorf("schema coverage: %w", err)
	}

	integEnv := append(os.Environ(),
		"INTEGRATION_TEST_DATABASE_URL="+env.hostDatabaseURLFor(integrationTestDB),
		"INTEGRATION_TEST_MIGRATIONS_DIR="+abs(migrationsDir),
		"INTEGRATION_TEST_GOOSE_PKG="+goosePackage,
		"FIREBASE_PROJECT_ID="+firebaseProject,
		"FIREBASE_AUTH_EMULATOR_HOST="+firebaseEmulatorHost,
	)
	if err := runWithEnv(integEnv, "go", "test", "-tags", "integration", "-count=1",
		"-cover", "-coverpkg", coverPkg, integrationTestDir,
		"-args", "-test.gocoverdir="+integDir); err != nil {
		return fmt.Errorf("integration coverage: %w", err)
	}

	if err := run("go", "tool", "covdata", "merge",
		"-i="+strings.Join([]string{unitDir, schemaDir, integDir}, ","), "-o="+mergedDir); err != nil {
		return fmt.Errorf("merge coverage: %w", err)
	}
	if err := run("go", "tool", "covdata", "textfmt", "-i="+mergedDir, "-o="+coverProfile); err != nil {
		return fmt.Errorf("render coverage profile: %w", err)
	}

	summary, total, err := summarizeCoverage(coverProfile)
	if err != nil {
		return err
	}
	if err := os.WriteFile(coverSummary, []byte(summary), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", coverSummary, err)
	}
	fmt.Print(summary)
	fmt.Printf("\nTotal (excluding generated): %.1f%% — profile: %s, summary: %s\n", total, coverProfile, coverSummary)
	return nil
}

// pkgCov accumulates covered/total statements for one package.
type pkgCov struct{ covered, total int }

// coverageIsGenerated reports whether a profile path is generated code we don't own
// (proto/sqlc output, mockery mocks, enumer String()/parse) and so excludes from the
// numbers — the same files .gremlins.yaml excludes from mutation.
func coverageIsGenerated(path string) bool {
	return strings.Contains(path, "/protogen/") ||
		strings.Contains(path, "/sqlcgen/") ||
		strings.Contains(path, "/mocks/") ||
		strings.HasSuffix(path, "_enumer.go")
}

// summarizeCoverage parses a `go tool covdata textfmt` profile and renders the markdown
// summary (per package, ascending, with a floor flag) plus the excl-generated total.
func summarizeCoverage(profile string) (string, float64, error) {
	f, err := os.Open(profile)
	if err != nil {
		return "", 0, fmt.Errorf("open %s: %w", profile, err)
	}
	defer f.Close()

	pkgs := map[string]*pkgCov{}
	var grandCov, grandTot int
	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first { // skip the "mode:" header
			first = false
			continue
		}
		// Each line is `<path>:<startLine>.<col>,<endLine>.<col> <numStmts> <count>`.
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		colon := strings.LastIndex(fields[0], ":")
		if colon < 0 {
			continue
		}
		path := fields[0][:colon]
		if coverageIsGenerated(path) {
			continue
		}
		stmts, err1 := strconv.Atoi(fields[1])
		count, err2 := strconv.Atoi(fields[2])
		slash := strings.LastIndex(path, "/")
		if err1 != nil || err2 != nil || slash < 0 {
			continue
		}
		pkg := path[:slash]
		pc := pkgs[pkg]
		if pc == nil {
			pc = &pkgCov{}
			pkgs[pkg] = pc
		}
		pc.total += stmts
		grandTot += stmts
		if count > 0 {
			pc.covered += stmts
			grandCov += stmts
		}
	}
	if err := sc.Err(); err != nil {
		return "", 0, err
	}

	type row struct {
		pkg string
		pct float64
	}
	rows := make([]row, 0, len(pkgs))
	for pkg, pc := range pkgs {
		pct := 0.0
		if pc.total > 0 {
			pct = 100 * float64(pc.covered) / float64(pc.total)
		}
		rows = append(rows, row{strings.TrimPrefix(pkg, "github.com/tallam99/qlab/backend/"), pct})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].pct < rows[j].pct })

	total := 0.0
	if grandTot > 0 {
		total = 100 * float64(grandCov) / float64(grandTot)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n## Coverage — %.1f%% (excluding generated code)\n\n", coverMarker, total)
	fmt.Fprintf(&b, "%d/%d statements across %d packages. Health floor: %.0f%% (⚠️ below).\n\n", grandCov, grandTot, len(rows), coverFloor)
	b.WriteString("| Package | Coverage |\n|---|---|\n")
	for _, r := range rows {
		flag := ""
		if r.pct < coverFloor {
			flag = " ⚠️"
		}
		fmt.Fprintf(&b, "| `%s` | %.1f%%%s |\n", r.pkg, r.pct, flag)
	}
	return b.String(), total, nil
}
