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

// ReadinessChecker reports whether the service's dependencies are reachable. It's
// the server's own (consumer-defined) view of readiness, kept separate from the
// data store interface so store holders may assume their store is already healthy.
// The Postgres store satisfies it.
type ReadinessChecker interface {
	Ready(ctx context.Context) error
}

// Options configures New. A struct (rather than positional params) so the server
// can take new dependencies (the data store, engine, auth) in later phases
// without churning call sites. Each field is optional; New supplies a sensible
// default for any left unset.
type Options struct {
	// Logger is the base logger; middleware derives request-scoped loggers from
	// it. Defaults to slog.Default().
	Logger *slog.Logger
	// Readiness backs the readiness probe. Defaults to a checker that always
	// reports ready — sensible for a server with no external dependencies to
	// verify. In this service the data store is passed here.
	Readiness ReadinessChecker
}

// Server holds the dependencies shared across handlers, all fully resolved.
type Server struct {
	logger    *slog.Logger
	readiness ReadinessChecker
}

// New builds the HTTP handler, resolving each Option to a concrete dependency
// (defaulting any left unset) so the Server owns everything it needs.
func New(opts Options) http.Handler {
	s := &Server{
		logger:    opts.Logger,
		readiness: opts.Readiness,
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	if s.readiness == nil {
		s.readiness = alwaysReady{}
	}

	r := chi.NewRouter()

	// Order matters — each line wraps everything below it:
	r.Use(middleware.RequestID)           // generate/propagate a per-request id (in ctx + response header)
	r.Use(httpmw.Recoverer(s.logger))     // turn downstream panics into a logged 500, never a crash
	r.Use(httpmw.RequestLogger(s.logger)) // one structured log line per request + request-scoped logger in ctx

	r.Get(pathHealthz, s.healthz) // liveness: is the process up?
	r.Get(pathReadyz, s.readyz)   // readiness: are its dependencies reachable?

	return r
}

// alwaysReady is the default ReadinessChecker: a server given no dependencies to
// verify has nothing that can be un-ready.
type alwaysReady struct{}

func (alwaysReady) Ready(context.Context) error { return nil }
