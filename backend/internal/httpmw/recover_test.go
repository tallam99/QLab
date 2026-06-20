//go:build testunit

package httpmw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"

	"github.com/tallam99/qlab/backend/internal/logging"
)

// noopLogger is a logging.Logger that discards everything, for quiet tests.
type noopLogger struct{}

func (noopLogger) Debug(string, ...any)       {}
func (noopLogger) Info(string, ...any)        {}
func (noopLogger) Warn(string, ...any)        {}
func (noopLogger) Error(string, ...any)       {}
func (noopLogger) With(...any) logging.Logger { return noopLogger{} }

// TestRecoverer verifies a panicking handler is turned into a 500 rather than
// propagating and crashing the server. RequestID is mounted first to mirror the
// real chain (Recoverer reads the id from chi).
func TestRecoverer(t *testing.T) {
	h := middleware.RequestID(
		Recoverer(noopLogger{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("boom")
		})),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	// Must not panic out of ServeHTTP.
	assert.NotPanics(t, func() { h.ServeHTTP(rec, req) })
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
