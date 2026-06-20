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

// fakeStore is a store.Store stub for tests. The interface is business-domain only
// (and currently empty), so it needs no methods.
type fakeStore struct{}

// TestReadyz verifies the readiness probe gates on startup: 503 until the server
// is marked ready, 200 afterward. Liveness (/healthz) is independent — see
// TestHealthz, which gets 200 from the same un-readied server.
func TestReadyz(t *testing.T) {
	s := New(Options{Logger: logging.Noop()})
	ts := httptest.NewServer(s)
	defer ts.Close()

	// Before dependencies are in place: not ready.
	resp, err := ts.Client().Get(ts.URL + pathReadyz)
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get(httpmw.HeaderRequestID))
	require.NoError(t, resp.Body.Close())

	// Attach the dependency and mark ready (what Run does after injectors run).
	s.store = fakeStore{}
	require.True(t, s.Ready())

	resp, err = ts.Client().Get(ts.URL + pathReadyz)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
}

// TestReady verifies the readiness gate: false until every required dependency is
// present, then latched true.
func TestReady(t *testing.T) {
	s := New(Options{Logger: logging.Noop()})
	assert.False(t, s.Ready(), "no store yet")

	s.store = fakeStore{}
	assert.True(t, s.Ready(), "store present")
	assert.True(t, s.Ready(), "stays ready (latched)")
}
