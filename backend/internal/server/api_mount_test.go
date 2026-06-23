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
// a POST to a QlabService procedure with no caller principal must reach the handler
// and come back as Connect's "unauthenticated" (HTTP 401) — not a 404, which would
// mean the service was never mounted. This is a wiring check (is the handler
// reachable, and is the auth gate in front of it?), not endpoint functionality;
// real behavior is covered by the Phase-7 integration suite.
func TestConnectServiceMounted(t *testing.T) {
	srv := httptest.NewServer(New(Options{Logger: logging.Noop()}))
	defer srv.Close()

	// A valid-uuid pool id so the request passes the protovalidate interceptor and
	// reaches the auth gate (an invalid id would stop at validation with 400).
	resp, err := srv.Client().Post(
		srv.URL+"/qlab.v1.QlabService/ListSlots",
		"application/json",
		strings.NewReader(`{"resourcePoolId":"11111111-1111-1111-1111-111111111111"}`),
	)
	require.NoError(t, err)
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()

	// Connect maps CodeUnauthenticated to HTTP 401; a 404 here would mean the route
	// isn't mounted at all.
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
