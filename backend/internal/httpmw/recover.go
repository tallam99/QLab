package httpmw

import (
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/tallam99/qlab/backend/internal/logging"
)

// log attribute keys; the one-off log message stays inline below.
const (
	attrPanic = "panic"
	attrStack = "stack"
)

// Recoverer recovers from panics in any downstream middleware or handler, logs
// the panic and stack (tagged with the chi request id), and responds 500 — so one
// bad request never takes the whole server down.
//
// Mount it early (right after RequestID) so it wraps everything below. It reads
// the request id from chi directly rather than via LoggerFromContext, because the
// request-scoped logger is set further down the chain and may not exist yet when
// an inner panic unwinds.
func Recoverer(base logging.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				// ErrAbortHandler is a sentinel for intentionally aborting a
				// handler; the stdlib expects it to propagate, not be logged. The
				// panic value is the exact sentinel (net/http re-panics it as-is,
				// never wrapped), so an identity compare is correct here.
				if rec == http.ErrAbortHandler { //nolint:errorlint // identity compare matches net/http; sentinel is never wrapped
					panic(rec)
				}

				base.Error("recovered from panic",
					attrRequestID, middleware.GetReqID(r.Context()),
					attrPanic, rec,
					attrStack, string(debug.Stack()),
				)
				w.WriteHeader(http.StatusInternalServerError)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
