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

// TestReadyz verifies the readiness probe returns 200 with the request-id header.
// Readiness is established at startup — the service serves only after every
// dependency initializes — so a reachable /readyz is always ready. This is an
// infrastructure check, not endpoint functionality.
func TestReadyz(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(Options{Logger: logger, Store: fakeStore{}}))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + pathReadyz)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get(httpmw.HeaderRequestID))
}
