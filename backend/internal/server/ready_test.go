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

// fakePinger is a stand-in for the DB pool so the readiness wiring can be tested
// without a real database.
type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

// TestReadyz verifies the readiness probe reflects database reachability: 200
// when the ping succeeds, 503 when it fails or no DB is wired. This is an
// infrastructure check (is the instance fit to receive traffic?), not endpoint
// functionality.
func TestReadyz(t *testing.T) {
	cases := []struct {
		name string
		db   Pinger
		want int
	}{
		{name: "db reachable", db: fakePinger{err: nil}, want: http.StatusOK},
		{name: "db unreachable", db: fakePinger{err: errors.New("refused")}, want: http.StatusServiceUnavailable},
		{name: "no db wired", db: nil, want: http.StatusServiceUnavailable},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(New(Options{Logger: logger, DB: tc.db}))
			defer srv.Close()

			resp, err := srv.Client().Get(srv.URL + pathReadyz)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tc.want, resp.StatusCode)
			assert.NotEmpty(t, resp.Header.Get(httpmw.HeaderRequestID))
		})
	}
}
