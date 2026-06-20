// Package server wires the HTTP router and middleware stack.
//
// Handlers are methods on Server so they share its dependencies (logger, etc.);
// cross-cutting concerns (request id, panic recovery, structured request
// logging) live in middleware. Connect-RPC handlers mount here in a later phase.
package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/tallam99/qlab/backend/internal/httpmw"
)

// Route paths, kept as consts so they have a single source of truth.
const (
	pathHealthz = "/healthz"
	pathReadyz  = "/readyz"
)

// Options configures New. A struct (rather than positional params) so the server
// can take new dependencies (the data store, engine, auth) in later phases
// without churning call sites.
type Options struct {
	// Logger is the base logger; middleware derives request-scoped loggers from it.
	Logger *slog.Logger
	// Ready reports whether the service's dependencies are reachable; the
	// readiness probe calls it. It's a plain check rather than the data store
	// because store holders may assume their store is already healthy (the data
	// store itself is wired in Phase 4). Required.
	Ready func(ctx context.Context) error
}

// Server holds the dependencies shared across handlers.
type Server struct {
	logger *slog.Logger
	ready  func(ctx context.Context) error
}

// New builds the HTTP handler with the middleware stack and routes wired in.
//
// It panics if a required dependency is missing: that is a programmer error at
// startup (a wiring bug), not a runtime condition — and it surfaces loudly rather
// than failing obscurely on the first request. The service cannot do anything
// useful without a store, so there is no degraded, store-less mode.
func New(opts Options) http.Handler {
	if opts.Logger == nil {
		panic("server: New requires a Logger")
	}
	if opts.Ready == nil {
		panic("server: New requires a Ready check")
	}
	s := &Server{logger: opts.Logger, ready: opts.Ready}

	r := chi.NewRouter()

	// Order matters — each line wraps everything below it:
	r.Use(middleware.RequestID)              // generate/propagate a per-request id (in ctx + response header)
	r.Use(httpmw.Recoverer(opts.Logger))     // turn downstream panics into a logged 500, never a crash
	r.Use(httpmw.RequestLogger(opts.Logger)) // one structured log line per request + request-scoped logger in ctx

	r.Get(pathHealthz, s.healthz) // liveness: is the process up?
	r.Get(pathReadyz, s.readyz)   // readiness: are its dependencies reachable?

	return r
}
