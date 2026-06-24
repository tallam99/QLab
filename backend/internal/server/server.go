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
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"firebase.google.com/go/v4/auth"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/tallam99/qlab/backend/internal/api"
	"github.com/tallam99/qlab/backend/internal/auth/firebaseauth"
	"github.com/tallam99/qlab/backend/internal/clients/postgres"
	"github.com/tallam99/qlab/backend/internal/dynamicqueue"
	"github.com/tallam99/qlab/backend/internal/dynamicqueue/basic"
	"github.com/tallam99/qlab/backend/internal/httpmw"
	"github.com/tallam99/qlab/backend/internal/logging"
	authenticationv1 "github.com/tallam99/qlab/backend/internal/services/authentication/v1"
	authzv1 "github.com/tallam99/qlab/backend/internal/services/authz/v1"
	notificationsv1 "github.com/tallam99/qlab/backend/internal/services/notifications/v1"
	"github.com/tallam99/qlab/backend/internal/services/scheduling"
	schedulingv1 "github.com/tallam99/qlab/backend/internal/services/scheduling/v1"
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
	// FirebaseAuth is the Firebase Auth client backing token verification. Required:
	// every data RPC is authenticated.
	FirebaseAuth *auth.Client
	// OperatorMount, when non-nil, mounts the staging/local-only operator surface
	// (qlab.dev.v1; decision 0008) at its Connect path. The caller builds it only
	// outside production — config refuses to load OPERATOR_* in production, so it is
	// never non-nil there.
	OperatorMount *OperatorMount
}

// OperatorMount is a built operator (qlab.dev.v1) Connect handler and the path to
// mount it on. Construction lives in the caller (main / the test harness), which
// owns the elevated DB connection it needs; the server just mounts it (non-prod only).
type OperatorMount struct {
	Path    string
	Handler http.Handler
}

// Server is the service: its HTTP handler plus the lifecycle that initializes
// dependencies and drains them on shutdown.
type Server struct {
	logger     logging.Logger
	handler    http.Handler
	httpServer *http.Server
	// apiService is the mounted Connect service; its scheduling dependency is
	// attached by the WithScheduling injector once the store is ready.
	apiService *api.Service
	// boundAddr is the listener's actual address, published once Run binds it (so
	// tests using a :0 port can discover it). Empty until then.
	boundAddr atomic.Pointer[string]
	// ready flips false->true once (Ready) when dependencies are initialized;
	// /readyq reflects it. Atomic because request goroutines read it.
	ready atomic.Bool
	// injectors initialize and attach runtime dependencies; Run executes them
	// after the listener is up.
	injectors []func(context.Context, *Server) error
	// closers are the injected dependencies that own resources; Run closes them on
	// shutdown (in registration order, after the HTTP server drains).
	closers []io.Closer
	// store is the data store, attached by an injector. Business handlers (Phase 5)
	// must read it only after loading ready, so the atomic acquire pairs with
	// Ready's release and they observe the write.
	store store.Store
	// firebaseAuth is the Firebase Auth client (from Options); the WithAuthentication
	// injector builds the verifier over it once the store is ready.
	firebaseAuth *auth.Client
}

// New builds the server and its middleware/route wiring. It panics if a required
// construction-time dependency is missing — a wiring bug should fail loudly.
func New(opts Options) *Server {
	if opts.Logger == nil {
		panic("server: New requires a Logger")
	}
	if opts.FirebaseAuth == nil {
		panic("server: New requires a FirebaseAuth client")
	}
	s := &Server{logger: opts.Logger, apiService: api.New(), firebaseAuth: opts.FirebaseAuth}

	r := chi.NewRouter()

	// Order matters — each line wraps everything below it. Authentication is NOT a
	// chi middleware: it is a Connect interceptor on the API handler (so it yields
	// native Connect error codes), which is why there is no principal middleware here.
	r.Use(httpmw.CORS(opts.AllowedOrigins))        // answer browser preflights / add CORS headers (outermost: skip logging OPTIONS noise)
	r.Use(middleware.RequestID)                    // generate/propagate a per-request id (in ctx + response header)
	r.Use(httpmw.Tracing(pathHealthq, pathReadyq)) // open a server span + extract inbound trace context (above RequestLogger, which reads its trace id)
	r.Use(httpmw.Recoverer(opts.Logger))           // turn downstream panics into a logged 500, never a crash
	r.Use(httpmw.RequestLogger(opts.Logger))       // one structured log line per request + request-scoped logger in ctx

	r.Get(pathHealthq, s.healthq) // liveness: is the process up? (always 200)
	r.Get(pathReadyq, s.readyq)   // readiness: have dependencies initialized?

	// Operator surface (qlab.dev.v1): staging/local only (decision 0008). Mounted
	// only when configured; absent entirely in production. It is a separate Connect
	// service with its own operator-secret gate, built by the caller.
	if opts.OperatorMount != nil {
		r.Mount(opts.OperatorMount.Path, opts.OperatorMount.Handler)
	}

	// Mount the Connect-RPC data API. The handler matches the full procedure paths
	// under apiPath, so it mounts cleanly alongside the health routes; its
	// scheduling dependency is attached by the WithScheduling injector during Run
	// (until then, methods return Unavailable).
	apiPath, apiHandler := s.apiService.Handler()
	r.Mount(apiPath, apiHandler)

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

// Run binds the listener and starts serving immediately (liveness up), initializes
// the registered dependencies, marks the service ready, and serves until ctx is
// cancelled or the listener fails. It always drains the HTTP server and closes
// dependencies before returning.
func (s *Server) Run(ctx context.Context) error {
	// Bind the listener synchronously, before the serve goroutine and the
	// injectors. This makes three things independent of how long dependency init
	// takes: liveness (/healthq) answers from the moment Run is past this line; a
	// port-bind failure surfaces here directly instead of being masked behind a
	// slow (or failing) database connect; and the deferred Shutdown can never race
	// a server that has not started listening yet.
	listener, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.httpServer.Addr, err)
	}
	// Publish the actual bound address so a caller that asked for a :0 (ephemeral)
	// port — e.g. the integration suite — can discover it.
	addr := listener.Addr().String()
	s.boundAddr.Store(&addr)

	serveErr := make(chan error, 1)
	go func() {
		s.logger.Info("server starting", attrAddr, s.httpServer.Addr)
		if err := s.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	// Don't log "ready; serving" if the listener has already failed (it was bound,
	// then errored mid-startup): surface that error instead of a misleading line.
	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	default:
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

// ConnectStore opens a Postgres pool at databaseURL and verifies it, retrying
// transient failures with bounded exponential backoff. A returned store is ready to
// use (it owns the pool — Close it to release); an error means the database stayed
// unreachable. This is the single connect-with-retry path shared by the main app
// store (WithPostgres) and the operator surface's elevated pool (decision 0008), so
// both ride out a Neon cold start identically instead of failing on the first ping.
func ConnectStore(ctx context.Context, logger logging.Logger, databaseURL string) (*pgstore.Store, error) {
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
			logger.Info("database connected")
			return dataStore, nil
		}
		if attempt >= dbInitAttempts {
			pool.Close()
			return nil, fmt.Errorf("database unreachable after %d attempts: %w", dbInitAttempts, err)
		}
		logger.Warn("database not ready; retrying",
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
		dataStore, err := ConnectStore(ctx, s.logger, databaseURL)
		if err != nil {
			return err
		}
		s.store = dataStore
		s.closers = append(s.closers, dataStore)
		return nil
	}
}

// WithAuthentication returns an injector that builds the authentication service
// (the Firebase token verifier over the ready store, which provisions invited users
// on first login) and attaches it to the mounted Connect handler's auth interceptor.
// Register it AFTER WithPostgres: it needs the store.
func WithAuthentication() func(context.Context, *Server) error {
	return func(_ context.Context, s *Server) error {
		if s.store == nil {
			return errors.New("WithAuthentication requires the store; register WithPostgres first")
		}
		if s.firebaseAuth == nil {
			return errors.New("WithAuthentication requires a Firebase Auth client")
		}
		// The store field is the (narrow) scheduling surface; recover the identity
		// surface from the same concrete store. The real pgstore satisfies both.
		authStore, ok := s.store.(store.AuthStore)
		if !ok {
			return errors.New("WithAuthentication requires a store that implements AuthStore")
		}
		authnService := authenticationv1.New(authenticationv1.Options{
			Verifier: firebaseauth.New(s.firebaseAuth),
			Store:    authStore,
		})
		s.apiService.SetAuthentication(authnService)
		return nil
	}
}

// WithSchedulingService returns an injector that builds the scheduling service
// (the basic engine + authorizer + notification builder over the ready store) and
// attaches it to the mounted Connect handler. Register it AFTER WithPostgres, which
// it depends on (the store must be set). clock may be nil — the service then uses
// the real time.Now; tests pass a controllable clock for deterministic time.
func WithSchedulingService(grace dynamicqueue.Minutes, clock scheduling.Clock) func(context.Context, *Server) error {
	return func(_ context.Context, s *Server) error {
		if s.store == nil {
			return errors.New("WithSchedulingService requires the store; register WithPostgres first")
		}
		schedulingService := schedulingv1.New(schedulingv1.Options{
			Store:         s.store,
			Engine:        basic.New(),
			Authorizer:    authzv1.New(s.store),
			Notifications: notificationsv1.New(),
			Clock:         clock,
			ClockInGrace:  grace,
			Logger:        s.logger,
		})
		s.apiService.SetScheduling(schedulingService)
		return nil
	}
}

// Addr returns the server's actual listen address once Run has bound it, or "" if
// it has not yet. Useful when Run was given a :0 (ephemeral) port.
func (s *Server) Addr() string {
	if a := s.boundAddr.Load(); a != nil {
		return *a
	}
	return ""
}
