// Package schematest holds database-level tests of the schema itself — that the
// constraints reject bad rows, the triggers fire, the enum types carry the
// expected labels, and the demo seed loads the expected values. They run against a
// throwaway database via `mage testSchema`, which creates it, applies all
// migrations, loads the seed, and drops it afterward.
//
// The tests carry the `database` build tag because they need a live Postgres, so
// ordinary `go build`/`go vet` and `mage testUnit` skip them. This file is
// deliberately untagged so the package is always a valid, buildable target (the
// directory is named schema_test; the package is schematest to avoid Go's special
// handling of the `_test` package suffix).
package schematest
