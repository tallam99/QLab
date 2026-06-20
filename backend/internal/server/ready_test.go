//go:build testunit

package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/httpmw"
)

// TestReadyz verifies the readiness probe reflects the readiness check: 200 when
// it passes, 503 when it fails. This is an infrastructure check (is the instance
// fit to receive traffic?), not endpoint functionality.
func TestReadyz(t *testing.T) {
	cases := []struct {
		name  string
		check error
		want  int
	}{
		{name: "dependencies reachable", check: nil, want: http.StatusOK},
		{name: "dependency unreachable", check: errors.New("refused"), want: http.StatusServiceUnavailable},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ready := func(context.Context) error { return tc.check }
			srv := httptest.NewServer(New(Options{Logger: logger, Ready: ready}))
			defer srv.Close()

			resp, err := srv.Client().Get(srv.URL + pathReadyz)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tc.want, resp.StatusCode)
			assert.NotEmpty(t, resp.Header.Get(httpmw.HeaderRequestID))
		})
	}
}
