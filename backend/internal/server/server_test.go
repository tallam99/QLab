//go:build testunit

package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/httpmw"
)

// Server-package tests are strictly infrastructural — server wiring, lifecycle,
// liveness/readiness. Endpoint functionality belongs in dedicated integration
// suites that exercise the full stack, not here.

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

// TestNewDefaultsReadiness verifies New supplies a sensible default for an unset
// option: with no Readiness configured, /readyz reports ready (a server with no
// dependencies to verify has nothing that can be un-ready).
func TestNewDefaultsReadiness(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(Options{Logger: logger})) // no Readiness set
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + pathReadyz)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
