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

// operatorMountFixture is a stand-in operator mount: a path and a trivial handler,
// enough to test mounting/guard behavior without the real operator service.
func operatorMountFixture() *OperatorMount {
	return &OperatorMount{
		Path: "/operator-probe/",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}
}

// TestNewRefusesOperatorInProduction is the load-bearing operator guard (decision
// 0008): the server must refuse to boot if the operator surface is enabled in
// production — the single most dangerous surface if it ever shipped to prod.
func TestNewRefusesOperatorInProduction(t *testing.T) {
	assert.PanicsWithValue(t, "server: operator surface must not be enabled in production", func() {
		New(Options{
			Logger:        logging.Noop(),
			FirebaseAuth:  testFirebaseAuth(t),
			Production:    true,
			OperatorMount: operatorMountFixture(),
		})
	})
}

// TestOperatorMountedOnlyWhenEnabled verifies the operator surface is reachable when
// configured and entirely absent (404) otherwise — so a production build
// (OperatorMount nil) has no operator surface at all.
func TestOperatorMountedOnlyWhenEnabled(t *testing.T) {
	// Disabled: nothing registered under the path, so a request 404s.
	off := httptest.NewServer(New(testOptions(t)))
	defer off.Close()
	resp, err := off.Client().Get(off.URL + "/operator-probe/x")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "operator surface must be absent when not configured")

	// Enabled: the mount handles the path (200 from the fixture handler).
	opts := testOptions(t)
	opts.OperatorMount = operatorMountFixture()
	on := httptest.NewServer(New(opts))
	defer on.Close()
	resp, err = on.Client().Get(on.URL + "/operator-probe/x")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusOK, resp.StatusCode, "operator surface must be mounted when configured")
}
