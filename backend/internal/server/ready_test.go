//go:build testunit

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/httpmw"
	"github.com/tallam99/qlab/backend/internal/logging"
)

// fakeStore is a store.Store stub for these lifecycle tests, which only need a
// non-nil dependency to attach — they never call its methods.
type fakeStore struct{}

func (fakeStore) CountLabs(context.Context) (int, error) { return 0, nil }

// TestReadyq verifies the readiness probe gates on startup: 503 until the server
// is marked ready, 200 afterward. Liveness (/healthq) is independent — see
// TestHealthq, which gets 200 from the same un-readied server.
func TestReadyq(t *testing.T) {
	s := New(Options{Logger: logging.Noop()})
	ts := httptest.NewServer(s)
	defer ts.Close()

	// Before dependencies are in place: not ready.
	resp, err := ts.Client().Get(ts.URL + pathReadyq)
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get(httpmw.HeaderRequestID))
	require.NoError(t, resp.Body.Close())

	// Attach the dependency and mark ready (what Run does after injectors run).
	s.store = fakeStore{}
	require.True(t, s.Ready())

	resp, err = ts.Client().Get(ts.URL + pathReadyq)
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
