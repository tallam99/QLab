//go:build testunit

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/httpmw"
	"github.com/tallam99/qlab/backend/internal/logging"
)

// Server-package tests are strictly infrastructural — server wiring, lifecycle,
// liveness/readiness. Endpoint functionality belongs in dedicated integration
// suites that exercise the full stack, not here.

// noopLogger is a logging.Logger that discards everything, for quiet tests.
type noopLogger struct{}

func (noopLogger) Debug(string, ...any)       {}
func (noopLogger) Info(string, ...any)        {}
func (noopLogger) Warn(string, ...any)        {}
func (noopLogger) Error(string, ...any)       {}
func (noopLogger) With(...any) logging.Logger { return noopLogger{} }

// TestNotFound checks the router's baseline behavior: an unknown path returns
// 404 and still carries the request-id header (so even misses are traceable).
func TestNotFound(t *testing.T) {
	srv := httptest.NewServer(testHandler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/does-not-exist")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get(httpmw.HeaderRequestID))
}

// TestNewRequiresDependencies verifies that a missing construction-time dependency
// is a loud, immediate failure (a wiring bug) rather than a nil-deref on first use.
func TestNewRequiresDependencies(t *testing.T) {
	assert.PanicsWithValue(t, "server: New requires a Logger", func() {
		New(Options{})
	})
}
