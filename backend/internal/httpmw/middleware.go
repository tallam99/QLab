// Package httpmw holds the service's HTTP middleware: structured per-request
// logging and panic recovery, both built on slog.
//
// A request id (from chi's RequestID middleware) is stamped on every log line
// so a single request's full story can be filtered out of the log stream and
// fed to Claude as a self-contained slice — the observability foundation called
// for in PLAN.md.
package httpmw

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// Response/log field names, kept as consts so they can be changed in one place
// and grepped across the codebase.
const (
	headerRequestID = "X-Request-Id"

	msgRequest = "http request"

	attrRequestID = "request_id"
	attrMethod    = "method"
	attrPath      = "path"
	attrStatus    = "status"
	attrBytes     = "bytes"
	attrDuration  = "duration"
)

// ctxKey is an unexported type for this package's context keys. Using a private
// type (rather than a bare string/int) guarantees no other package can collide
// with or read our context values by accident.
type ctxKey int

// loggerKey is the context key under which RequestLogger stores the
// request-scoped *slog.Logger for handlers to retrieve via FromContext.
const loggerKey ctxKey = iota

// RequestLogger emits one structured log line per request — tagged with the chi
// request id, method, path, status, bytes, and duration — and stashes a
// request-scoped logger carrying that id in the context for handlers to reuse.
//
// It expects chi's RequestID middleware to run before it.
func RequestLogger(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := middleware.GetReqID(r.Context())
			l := base.With(slog.String(attrRequestID, reqID))

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			// Echo the id on the response so a client (or Claude) can correlate a
			// response with its log line. Must be set before the handler writes.
			ww.Header().Set(headerRequestID, reqID)

			r = r.WithContext(context.WithValue(r.Context(), loggerKey, l))
			next.ServeHTTP(ww, r)

			l.LogAttrs(r.Context(), slog.LevelInfo, msgRequest,
				slog.String(attrMethod, r.Method),
				slog.String(attrPath, r.URL.Path),
				slog.Int(attrStatus, ww.Status()),
				slog.Int(attrBytes, ww.BytesWritten()),
				slog.Duration(attrDuration, time.Since(start)),
			)
		})
	}
}

// FromContext returns the request-scoped logger placed by RequestLogger.
//
// It panics if the logger is absent: by construction RequestLogger runs on every
// route, so a missing logger means FromContext was called outside a request —
// a programmer error. The Recoverer middleware turns that panic into a 500 and a
// logged stack rather than crashing the server.
func FromContext(ctx context.Context) *slog.Logger {
	l, ok := ctx.Value(loggerKey).(*slog.Logger)
	if !ok {
		panic("httpmw: no request logger in context (is RequestLogger middleware mounted?)")
	}
	return l
}
