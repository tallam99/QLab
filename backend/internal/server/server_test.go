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
)

// TestRoutes exercises the wired router end-to-end (middleware + handler) so we
// catch wiring regressions, not just the handler in isolation. /healthz is the
// only route in this phase; the table grows with the API.
func TestRoutes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(logger))
	defer srv.Close()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   map[string]string
	}{
		{
			name:       "healthz returns ok",
			method:     http.MethodGet,
			path:       "/healthz",
			wantStatus: http.StatusOK,
			wantBody:   map[string]string{"status": "ok"},
		},
		{
			name:       "unknown route 404s",
			method:     http.MethodGet,
			path:       "/nope",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, srv.URL+tt.path, nil)
			require.NoError(t, err)

			resp, err := srv.Client().Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
			// chi's RequestID middleware must stamp every response so a request's
			// logs can be correlated — the observability foundation for this build.
			assert.NotEmpty(t, resp.Header.Get("X-Request-Id"))

			if tt.wantBody != nil {
				var got map[string]string
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
				assert.Equal(t, tt.wantBody, got)
			}
		})
	}
}
