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

// fakeStore is a store.Store stub for tests. The interface is business-domain only
// (and currently empty), so it needs no methods.
type fakeStore struct{}

// TestReadyz verifies the readiness probe gates on startup: 503 until MarkReady,
// 200 afterward. Liveness (/healthz) is independent of this — see TestHealthz,
// which gets 200 from the same un-readied server. Infrastructure check, not
// endpoint functionality.
func TestReadyz(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(Options{Logger: logger})
	ts := httptest.NewServer(s)
	defer ts.Close()

	// Before dependencies initialize: not ready.
	resp, err := ts.Client().Get(ts.URL + pathReadyz)
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get(httpmw.HeaderRequestID))
	require.NoError(t, resp.Body.Close())

	// After MarkReady: ready.
	s.MarkReady(fakeStore{})
	resp, err = ts.Client().Get(ts.URL + pathReadyz)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
}
