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

// fakeStore is a store.Store stub whose readiness the test controls. It stands in
// for any implementation a caller might inject (real, no-op, or failing).
type fakeStore struct{ ready error }

func (f fakeStore) Ready(context.Context) error { return f.ready }

// TestReadyz verifies the readiness probe reflects the store's readiness: 200 when
// the store reports ready, 503 when it doesn't. This is an infrastructure check
// (is the instance fit to receive traffic?), not endpoint functionality.
func TestReadyz(t *testing.T) {
	cases := []struct {
		name  string
		ready error
		want  int
	}{
		{name: "store reachable", ready: nil, want: http.StatusOK},
		{name: "store unreachable", ready: errors.New("refused"), want: http.StatusServiceUnavailable},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(New(Options{Logger: logger, Store: fakeStore{ready: tc.ready}}))
			defer srv.Close()

			resp, err := srv.Client().Get(srv.URL + pathReadyz)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tc.want, resp.StatusCode)
			assert.NotEmpty(t, resp.Header.Get(httpmw.HeaderRequestID))
		})
	}
}
