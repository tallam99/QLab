// Package httpmw holds the service's HTTP middleware: structured per-request
// logging and panic recovery.
//
// A request id (from chi's RequestID middleware) is stamped on every log line
// so a single request's full story can be filtered out of the log stream and
// fed to Claude as a self-contained slice — the observability foundation called
// for in PLAN.md.
package httpmw

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/tallam99/qlab/backend/internal/logging"
)

// Request/response header and log attribute keys, kept as consts so they're
// spelled identically across log sites and grep-able. (One-off log messages stay
// inline.)
const (
	// HeaderRequestID is the response header carrying the per-request id; exported
	// so tests and clients can reference the canonical spelling.
	HeaderRequestID = "X-Request-Id"
	// headerForwardedFor is the proxy-supplied client address chain.
	headerForwardedFor = "X-Forwarded-For"

	attrRequestID    = "request_id"
	attrMethod       = "method"
	attrPath         = "path"
	attrStatus       = "status"
	attrBytes        = "bytes"
	attrDuration     = "duration"
	attrRemoteAddr   = "remote_addr"
	attrForwardedFor = "forwarded_for"
)

// ctxKey is an unexported type for this package's context keys. Using a private
// type (rather than a bare string/int) guarantees no other package can collide
// with or read our context values by accident.
type ctxKey int

// loggerKey is the context key under which RequestLogger stores the
// request-scoped Logger for handlers to retrieve via LoggerFromContext.
const loggerKey ctxKey = iota

// RequestLogger emits one structured log line per request — tagged with the chi
// request id, method, path, status, bytes, and duration — and stashes a
// request-scoped logger carrying that id in the context for handlers to reuse.
//
// It expects chi's RequestID middleware to run before it.
func RequestLogger(base logging.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := middleware.GetReqID(r.Context())
			l := base.With(attrRequestID, reqID)

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			// Echo the id on the response so a client (or Claude) can correlate a
			// response with its log line. Must be set before the handler writes.
			ww.Header().Set(HeaderRequestID, reqID)

			r = r.WithContext(context.WithValue(r.Context(), loggerKey, l))
			next.ServeHTTP(ww, r)

			// Log the real peer (r.RemoteAddr), plus the raw X-Forwarded-For chain
			// when present. We deliberately do NOT trust a forwarded header to
			// rewrite the client address (that is the spoofing flaw that deprecated
			// chi's RealIP): canonical client-IP attribution needs a trusted-proxy
			// config, which arrives with the Cloud Run topology.
			args := []any{
				attrMethod, r.Method,
				attrPath, r.URL.Path,
				attrStatus, ww.Status(),
				attrBytes, ww.BytesWritten(),
				attrDuration, time.Since(start),
				attrRemoteAddr, remoteHost(r.RemoteAddr),
			}
			if xff := r.Header.Get(headerForwardedFor); xff != "" {
				args = append(args, attrForwardedFor, xff)
			}
			l.Info("http request", args...)
		})
	}
}

// LoggerFromContext returns the request-scoped logger placed by RequestLogger.
//
// It panics if the logger is absent: by construction RequestLogger runs on every
// route, so a missing logger means it was called outside a request — a programmer
// error. The Recoverer middleware turns that panic into a 500 and a logged stack
// rather than crashing the server.
func LoggerFromContext(ctx context.Context) logging.Logger {
	l, ok := ctx.Value(loggerKey).(logging.Logger)
	if !ok {
		panic("httpmw: no request logger in context (is RequestLogger middleware mounted?)")
	}
	return l
}

// remoteHost strips the port from a "host:port" RemoteAddr, returning the address
// unchanged if it has no port.
func remoteHost(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}
