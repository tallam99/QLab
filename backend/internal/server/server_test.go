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

// TestNotFound checks the router's baseline behavior: an unknown path returns
// 404 and still carries the request-id header (so even misses are traceable).
func TestNotFound(t *testing.T) {
	srv := httptest.NewServer(testHandler(t))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/does-not-exist")
	require.NoError(t, err)
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get(httpmw.HeaderRequestID))
}

// TestNewRequiresDependencies verifies that a missing construction-time dependency
// is a loud, immediate failure (a wiring bug) rather than a nil-deref on first use.
func TestNewRequiresDependencies(t *testing.T) {
	assert.PanicsWithValue(t, "server: New requires a Logger", func() {
		New(Options{})
	})
	assert.PanicsWithValue(t, "server: New requires a FirebaseAuth client", func() {
		New(Options{Logger: logging.Noop()})
	})
}

// TestNewRefusesDevLoginInProduction is the load-bearing dev-auth guard (decision
// 0007): the server must refuse to boot if the dev-login endpoint is enabled in
// production — the single most dangerous surface if it ever shipped to prod.
func TestNewRefusesDevLoginInProduction(t *testing.T) {
	assert.PanicsWithValue(t, "server: dev-login must not be enabled in production", func() {
		New(Options{
			Logger:       logging.Noop(),
			FirebaseAuth: testFirebaseAuth(t),
			Production:   true,
			DevLogin:     &DevLoginConfig{},
		})
	})
}

// TestDevLoginMountedOnlyWhenEnabled verifies the route exists when configured and
// is entirely absent (404) otherwise — so a production build (DevLogin nil) has no
// dev-login surface at all.
func TestDevLoginMountedOnlyWhenEnabled(t *testing.T) {
	// Disabled: the route is not registered, so a request 404s.
	off := httptest.NewServer(New(testOptions(t)))
	defer off.Close()
	resp, err := off.Client().Post(off.URL+pathDevLogin, "application/json", http.NoBody)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "dev-login must be absent when not configured")

	// Enabled: the route is registered. An empty body fails the handler's own
	// validation (400), proving the route exists (not 404).
	opts := testOptions(t)
	opts.DevLogin = &DevLoginConfig{}
	on := httptest.NewServer(New(opts))
	defer on.Close()
	resp, err = on.Client().Post(on.URL+pathDevLogin, "application/json", http.NoBody)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.NotEqual(t, http.StatusNotFound, resp.StatusCode, "dev-login must be mounted when configured")
}
