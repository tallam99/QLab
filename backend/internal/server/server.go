// Package server wires the HTTP router and middleware stack.
//
// Handlers are methods on Server so they share its dependencies; cross-cutting
// concerns (request id, panic recovery, structured request logging) live in
// middleware. Connect-RPC handlers mount here in a later phase.
//
// The server starts serving before its runtime dependencies are initialized, so
// liveness (/healthz) is up immediately; readiness (/readyz) stays not-ready
// until MarkReady is called once those dependencies are ready.
package server

import (
	"log/slog"
	"net/http"
	"sync/atomic"

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

// Options configures New with the dependencies available at construction time.
// Runtime dependencies that must be initialized first (the data store, and later
// cache/queue/bucket/…) are injected afterward via MarkReady — the server starts
// listening, so liveness is up, before those dependencies exist.
type Options struct {
	// Logger is the base logger; middleware derives request-scoped loggers from it.
	Logger *slog.Logger
}

// Server is the service's HTTP handler. It serves immediately (so /healthz reports
// the process is live) and reports /readyz not-ready until MarkReady runs.
type Server struct {
	logger  *slog.Logger
	handler http.Handler
	// ready flips false->true exactly once, in MarkReady, when dependencies are
	// initialized; /readyz reflects it. Atomic because request goroutines read it.
	ready atomic.Bool
	// store is injected by MarkReady (before ready is set). Business handlers
	// (Phase 4) must read it only after loading ready, so the atomic acquire pairs
	// with MarkReady's release and they observe this write.
	store store.Store
}

// New builds the server and its middleware/route wiring. It panics if a required
// construction-time dependency is missing — a wiring bug should fail loudly, not
// nil-deref on the first request.
func New(opts Options) *Server {
	if opts.Logger == nil {
		panic("server: New requires a Logger")
	}
	s := &Server{logger: opts.Logger}

	r := chi.NewRouter()

	// Order matters — each line wraps everything below it:
	r.Use(middleware.RequestID)              // generate/propagate a per-request id (in ctx + response header)
	r.Use(httpmw.Recoverer(opts.Logger))     // turn downstream panics into a logged 500, never a crash
	r.Use(httpmw.RequestLogger(opts.Logger)) // one structured log line per request + request-scoped logger in ctx

	r.Get(pathHealthz, s.healthz) // liveness: is the process up? (always 200)
	r.Get(pathReadyz, s.readyz)   // readiness: have dependencies initialized?

	s.handler = r
	return s
}

// ServeHTTP makes Server an http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// MarkReady records the service's runtime dependencies and flips /readyz to ready.
// main calls it once, after every dependency has initialized successfully.
func (s *Server) MarkReady(st store.Store) {
	s.store = st
	s.ready.Store(true)
}
