package httpmw

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5/middleware"
)

const (
	msgPanic = "recovered from panic"

	attrPanic = "panic"
	attrStack = "stack"
)

// Recoverer recovers from panics in any downstream middleware or handler, logs
// the panic and stack via slog (tagged with the chi request id), and responds
// 500 — so one bad request never takes the whole server down.
//
// Mount it early (right after RequestID) so it wraps everything below. It reads
// the request id from chi directly rather than via FromContext, because the
// request-scoped logger is set further down the chain and may not exist yet when
// an inner panic unwinds.
func Recoverer(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				// ErrAbortHandler is a sentinel for intentionally aborting a
				// handler; the stdlib expects it to propagate, not be logged.
				if rec == http.ErrAbortHandler {
					panic(rec)
				}

				base.LogAttrs(r.Context(), slog.LevelError, msgPanic,
					slog.String(attrRequestID, middleware.GetReqID(r.Context())),
					slog.Any(attrPanic, rec),
					slog.String(attrStack, string(debug.Stack())),
				)
				w.WriteHeader(http.StatusInternalServerError)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
