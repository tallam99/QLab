// Package server wires the HTTP router, middleware, and the service lifecycle.
//
// The Server owns its lifecycle: Run starts serving before dependencies are
// initialized (so liveness is up immediately), runs the registered dependency
// injectors, marks itself ready, then drains and closes everything on shutdown.
// Handlers are methods on Server so they share its dependencies; cross-cutting
// concerns (request id, panic recovery, structured logging) live in middleware.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/tallam99/qlab/backend/internal/clients/postgres"
	"github.com/tallam99/qlab/backend/internal/httpmw"
	"github.com/tallam99/qlab/backend/internal/logging"
	"github.com/tallam99/qlab/backend/internal/store"
	"github.com/tallam99/qlab/backend/internal/store/pgstore"
)

// Route paths, kept as consts so they have a single source of truth. The "q"
// suffix is for QLab and deliberately avoids "/healthz": Google Cloud Run's front
// end intercepts /healthz and answers it itself, so a request to /healthz never
// reaches this service (it returns a Google 404). /healthq and /readyq are not
// reserved, so they reach our handlers.
const (
	pathHealthq = "/healthq"
	pathReadyq  = "/readyq"
)

const (
	// readHeaderTimeout bounds how long a client may take to send request headers
	// (a cheap slowloris guard).
	readHeaderTimeout = 5 * time.Second
	// shutdownTimeout bounds graceful drain of in-flight requests on shutdown.
	shutdownTimeout = 10 * time.Second
)

// Dependency-init retry. The goal: ride out a transient failure (e.g. a Neon
// database waking from scale-to-zero) without waiting long on a real outage.
const (
	dbInitAttempts  = 5
	dbInitBaseDelay = 500 * time.Millisecond
	dbInitMaxDelay  = 4 * time.Second
	dbPingTimeout   = 5 * time.Second
)

// Reused log attribute keys.
const (
	attrError = "error"
	attrAddr  = "addr"
)

// Options configures New with the dependencies available at construction time.
// Runtime dependencies that must be initialized first are registered via
// InjectDependency and wired during Run — the server listens (so liveness is up)
// before they exist.
type Options struct {
	// Logger is the base logger; middleware derives request-scoped loggers from it.
	Logger logging.Logger
	// Addr is the TCP listen address (e.g. ":8090").
	Addr string
	// AllowedOrigins is the CORS allow-list for the browser PWA (a separate origin
	// from the API; decision 0001). Empty means same-origin only.
	AllowedOrigins []string
}

// Server is the service: its HTTP handler plus the lifecycle that initializes
// dependencies and drains them on shutdown.
type Server struct {
	logger     logging.Logger
	handler    http.Handler
	httpServer *http.Server
	// ready flips false->true once (Ready) when dependencies are initialized;
	// /readyq reflects it. Atomic because request goroutines read it.
	ready atomic.Bool
	// injectors initialize and attach runtime dependencies; Run executes them
	// after the listener is up.
	injectors []func(context.Context, *Server) error
	// closers are the injected dependencies that own resources; Run closes them on
	// shutdown (in registration order, after the HTTP server drains).
	closers []io.Closer
	// store is the data store, attached by an injector. Business handlers (Phase 4)
	// must read it only after loading ready, so the atomic acquire pairs with
	// Ready's release and they observe the write.
	store store.Store
}

// New builds the server and its middleware/route wiring. It panics if a required
// construction-time dependency is missing — a wiring bug should fail loudly.
func New(opts Options) *Server {
	if opts.Logger == nil {
		panic("server: New requires a Logger")
	}
	s := &Server{logger: opts.Logger}

	r := chi.NewRouter()

	// Order matters — each line wraps everything below it:
	r.Use(httpmw.CORS(opts.AllowedOrigins))  // answer browser preflights / add CORS headers (outermost: skip logging OPTIONS noise)
	r.Use(middleware.RequestID)              // generate/propagate a per-request id (in ctx + response header)
	r.Use(httpmw.Recoverer(opts.Logger))     // turn downstream panics into a logged 500, never a crash
	r.Use(httpmw.RequestLogger(opts.Logger)) // one structured log line per request + request-scoped logger in ctx

	r.Get(pathHealthq, s.healthq) // liveness: is the process up? (always 200)
	r.Get(pathReadyq, s.readyq)   // readiness: have dependencies initialized?

	s.handler = r
	s.httpServer = &http.Server{
		Addr:              opts.Addr,
		Handler:           r,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	return s
}

// ServeHTTP makes Server an http.Handler (used by tests; Run uses the http.Server).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// InjectDependency registers a function that initializes and attaches a runtime
// dependency. Run executes the registered injectors in order, after the listener
// is up — so adding a dependency is one InjectDependency call, not a new method.
func (s *Server) InjectDependency(inject func(context.Context, *Server) error) {
	s.injectors = append(s.injectors, inject)
}

// Ready reports whether every dependency needed to serve traffic is in place,
// latching the result so /readyq flips on. It short-circuits once latched.
func (s *Server) Ready() bool {
	if s.ready.Load() {
		return true
	}
	if s.store == nil {
		return false
	}
	s.ready.Store(true)
	return true
}

// Run starts serving immediately (liveness up), initializes the registered
// dependencies, marks the service ready, and serves until ctx is cancelled or the
// listener fails. It always drains the HTTP server and closes dependencies before
// returning.
func (s *Server) Run(ctx context.Context) error {
	serveErr := make(chan error, 1)
	go func() {
		s.logger.Info("server starting", attrAddr, s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	// Drain then close, in that order (LIFO): stop accepting requests first, so
	// in-flight handlers finish before their dependencies' resources go away.
	defer s.closeDependencies()
	defer s.shutdownServer()

	for _, inject := range s.injectors {
		if err := inject(ctx, s); err != nil {
			return fmt.Errorf("initialize dependencies: %w", err)
		}
	}
	if !s.Ready() {
		return errors.New("dependencies initialized but server is not ready")
	}
	s.logger.Info("ready; serving")

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		s.logger.Info("server shutting down")
		return nil
	}
}

// shutdownServer drains in-flight requests within shutdownTimeout.
func (s *Server) shutdownServer() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.logger.Error("graceful shutdown failed", attrError, err)
	}
}

// closeDependencies releases every injected dependency that owns resources.
func (s *Server) closeDependencies() {
	for _, c := range s.closers {
		if err := c.Close(); err != nil {
			s.logger.Error("close dependency failed", attrError, err)
		}
	}
}

// initPgStore opens the Postgres pool and verifies it, retrying transient
// failures with bounded exponential backoff. A returned store is ready to use; an
// error means the database stayed unreachable.
func (s *Server) initPgStore(ctx context.Context, databaseURL string) (*pgstore.Store, error) {
	pool, err := postgres.New(ctx, postgres.Options{DatabaseURL: databaseURL})
	if err != nil {
		return nil, fmt.Errorf("open database pool: %w", err)
	}

	delay := dbInitBaseDelay
	for attempt := 1; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, dbPingTimeout)
		dataStore, err := pgstore.New(pingCtx, pool)
		cancel()
		if err == nil {
			s.logger.Info("database connected")
			return dataStore, nil
		}
		if attempt >= dbInitAttempts {
			pool.Close()
			return nil, fmt.Errorf("database unreachable after %d attempts: %w", dbInitAttempts, err)
		}
		s.logger.Warn("database not ready; retrying",
			attrError, err, "attempt", attempt, "retry_in", delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		}
		delay = min(delay*2, dbInitMaxDelay)
	}
}

// WithPostgres returns an injector that connects the Postgres store and attaches
// it to the server. Pass it to InjectDependency.
func WithPostgres(databaseURL string) func(context.Context, *Server) error {
	return func(ctx context.Context, s *Server) error {
		dataStore, err := s.initPgStore(ctx, databaseURL)
		if err != nil {
			return err
		}
		s.store = dataStore
		s.closers = append(s.closers, dataStore)
		return nil
	}
}
