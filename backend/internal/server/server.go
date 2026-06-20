// Package server wires the HTTP router and middleware stack.
//
// Handlers are methods on Server so they share its dependencies; cross-cutting
// concerns (request id, panic recovery, structured request logging) live in
// middleware. Connect-RPC handlers mount here in a later phase.
package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/tallam99/qlab/backend/internal/httpmw"
	"github.com/tallam99/qlab/backend/internal/store"
)

// Route paths, kept as consts so they have a single source of truth.
const (
	pathHealthz = "/healthz"
	pathReadyz  = "/readyz"
)

// Options configures New. Its fields are the service's dependencies, typed as
// interfaces so callers can swap real, stub, or no-op implementations freely.
// They are passed already constructed and ready — the service uses them through
// their interfaces, it does not configure them. (Cache, queue, bucket, … join
// here as the service needs them.)
type Options struct {
	// Logger is the base logger; middleware derives request-scoped loggers from
	// it. Required.
	Logger *slog.Logger
	// Store is the data store the handlers use; the readiness probe asks it whether
	// it's reachable. Required.
	Store store.Store
}

// Server holds the service's dependencies, shared across handlers.
type Server struct {
	logger *slog.Logger
	store  store.Store
}

// New builds the HTTP handler with the middleware stack and routes wired in. It
// panics if a required dependency is missing — a wiring bug should fail loudly at
// startup, not nil-deref on the first request.
func New(opts Options) http.Handler {
	if opts.Logger == nil {
		panic("server: New requires a Logger")
	}
	if opts.Store == nil {
		panic("server: New requires a Store")
	}
	s := &Server{logger: opts.Logger, store: opts.Store}

	r := chi.NewRouter()

	// Order matters — each line wraps everything below it:
	r.Use(middleware.RequestID)              // generate/propagate a per-request id (in ctx + response header)
	r.Use(httpmw.Recoverer(opts.Logger))     // turn downstream panics into a logged 500, never a crash
	r.Use(httpmw.RequestLogger(opts.Logger)) // one structured log line per request + request-scoped logger in ctx

	r.Get(pathHealthz, s.healthz) // liveness: is the process up?
	r.Get(pathReadyz, s.readyz)   // readiness: are its dependencies reachable?

	return r
}
