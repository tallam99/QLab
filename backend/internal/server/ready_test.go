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

// fakeStore is a stand-in for the data store so the readiness wiring can be
// tested without a real database.
type fakeStore struct{ err error }

func (f fakeStore) Ping(context.Context) error { return f.err }

// TestReadyz verifies the readiness probe reflects store reachability: 200 when
// the ping succeeds, 503 when it fails. This is an infrastructure check (is the
// instance fit to receive traffic?), not endpoint functionality.
func TestReadyz(t *testing.T) {
	cases := []struct {
		name string
		ping error
		want int
	}{
		{name: "store reachable", ping: nil, want: http.StatusOK},
		{name: "store unreachable", ping: errors.New("refused"), want: http.StatusServiceUnavailable},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(New(Options{Logger: logger, Store: fakeStore{err: tc.ping}}))
			defer srv.Close()

			resp, err := srv.Client().Get(srv.URL + pathReadyz)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tc.want, resp.StatusCode)
			assert.NotEmpty(t, resp.Header.Get(httpmw.HeaderRequestID))
		})
	}
}
