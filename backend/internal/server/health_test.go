//go:build testunit

package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/httpmw"
)

// testHandler builds a server with discarded logs and a stub store.
func testHandler() http.Handler {
	return New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Store:  fakeStore{},
	})
}

// TestHealthz verifies the liveness probe: it must return 200 with the ok body
// and the request-id response header. This is an infrastructure check (is the
// server up and wired?), not endpoint functionality — that lives in integration
// suites once real endpoints exist.
func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(testHandler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + pathHealthz)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get(httpmw.HeaderRequestID))

	var got map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, map[string]string{"status": "ok"}, got)
}
