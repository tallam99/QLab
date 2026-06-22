//go:build testunit

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/logging"
)

// TestConnectServiceMounted verifies the Connect data API is wired onto the router:
// a POST to a QlabService procedure must reach the (stubbed) handler and come back
// as Connect's "unimplemented" (HTTP 501) — not a 404, which would mean the service
// was never mounted. This is a wiring check (is the handler reachable?), not
// endpoint functionality; the real behavior lands with the Phase 7 handlers and is
// covered by integration suites then.
func TestConnectServiceMounted(t *testing.T) {
	srv := httptest.NewServer(New(Options{Logger: logging.Noop()}))
	defer srv.Close()

	resp, err := srv.Client().Post(
		srv.URL+"/qlab.v1.QlabService/ListSlots",
		"application/json",
		strings.NewReader(`{"resourcePoolId":"demo"}`),
	)
	require.NoError(t, err)
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()

	// Connect maps CodeUnimplemented to HTTP 501; a 404 here would mean the route
	// isn't mounted at all.
	assert.Equal(t, http.StatusNotImplemented, resp.StatusCode)
}
