// Package server wires the HTTP router and middleware stack.
//
// Handlers stay thin; cross-cutting concerns (request id, panic recovery,
// structured request logging) live in middleware. Connect-RPC handlers mount
// here in a later phase — chi gives a clean place to hang shared middleware.
package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/tallam99/qlab/backend/internal/httpmw"
)

// Route paths, kept as consts so they have a single source of truth.
const pathHealthz = "/healthz"

// Options configures New. A struct (rather than positional params) so the server
// can take new dependencies (DB, engine, auth) in later phases without churning
// call sites — the project convention for constructors.
type Options struct {
	// Logger is the base logger; middleware derives request-scoped loggers from it.
	Logger *slog.Logger
}

// New builds the HTTP handler with the middleware stack and routes wired in.
func New(opts Options) http.Handler {
	r := chi.NewRouter()

	// Order matters — each line wraps everything below it:
	r.Use(middleware.RequestID)              // generate/propagate a per-request id (in ctx + response header)
	r.Use(httpmw.Recoverer(opts.Logger))     // turn downstream panics into a logged 500, never a crash
	r.Use(middleware.RealIP)                 // trust X-Forwarded-For/X-Real-IP so logs show the client ip
	r.Use(httpmw.RequestLogger(opts.Logger)) // one structured log line per request + request-scoped logger in ctx

	r.Get(pathHealthz, healthz)

	return r
}
