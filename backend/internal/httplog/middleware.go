// Package httplog provides request-scoped structured logging.
//
// A request id (from chi's RequestID middleware) is stamped on every log line
// so a single request's full story can be filtered out of the log stream and
// fed to Claude as a self-contained slice — the observability foundation called
// for in PLAN.md.
package httplog

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

type ctxKey int

const loggerKey ctxKey = iota

// Middleware emits one structured log line per request — tagged with the chi
// request id, method, path, status, bytes, and duration — and stashes a
// request-scoped logger carrying that id in the context for handlers to reuse.
//
// It expects chi's RequestID middleware to run before it.
func Middleware(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := middleware.GetReqID(r.Context())
			l := base.With(slog.String("request_id", reqID))

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			// Echo the id on the response so a client (or Claude) can correlate a
			// response with its log line. Must be set before the handler writes.
			ww.Header().Set("X-Request-Id", reqID)

			r = r.WithContext(context.WithValue(r.Context(), loggerKey, l))
			next.ServeHTTP(ww, r)

			l.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// FromContext returns the request-scoped logger placed by Middleware, falling
// back to slog.Default if called outside a request.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
