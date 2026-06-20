package httpmw

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
)

// TestRecoverer verifies a panicking handler is turned into a 500 rather than
// propagating and crashing the server. RequestID is mounted first to mirror the
// real chain (Recoverer reads the id from chi).
func TestRecoverer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	h := middleware.RequestID(
		Recoverer(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("boom")
		})),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	// Must not panic out of ServeHTTP.
	assert.NotPanics(t, func() { h.ServeHTTP(rec, req) })
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
