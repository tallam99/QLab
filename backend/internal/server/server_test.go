//go:build testunit

package server

import (
	"context"
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

// TestNewRequiresDependencies verifies that missing required dependencies are a
// loud, immediate failure (a wiring bug) rather than a nil-deref on first use.
func TestNewRequiresDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ready := func(context.Context) error { return nil }

	assert.PanicsWithValue(t, "server: New requires a Logger", func() {
		New(Options{Ready: ready})
	})
	assert.PanicsWithValue(t, "server: New requires a Ready check", func() {
		New(Options{Logger: logger})
	})
}
