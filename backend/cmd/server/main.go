// Command server is the QLab data API entrypoint.
//
// It stays thin by design: load config, build the logger, start serving (so
// liveness is up immediately), initialize dependencies with bounded retry, mark
// the service ready, and serve until a shutdown signal. All logic lives in
// internal/.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tallam99/qlab/backend/internal/clients/postgres"
	"github.com/tallam99/qlab/backend/internal/config"
	"github.com/tallam99/qlab/backend/internal/logging"
	"github.com/tallam99/qlab/backend/internal/server"
	"github.com/tallam99/qlab/backend/internal/store/pgstore"
)

const (
	// readHeaderTimeout bounds how long a client may take to send request headers
	// (a cheap slowloris guard).
	readHeaderTimeout = 5 * time.Second
	// shutdownTimeout bounds graceful drain of in-flight requests on SIGTERM.
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

// Log attribute keys (reused across log sites, so kept as consts for a single
// spelling). One-off log messages stay inline below.
const (
	attrError = "error"
	attrAddr  = "addr"
	attrEnv   = "env"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", slog.Any(attrError, err))
		os.Exit(1)
	}
}

func run() error {
	// Bootstrap logger (JSON) so a failure before config is loaded is still
	// structured in Cloud Logging; replaced once the environment is known.
	slog.SetDefault(logging.New(logging.Options{Local: false, Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Verbose locally, info and above in the cloud.
	logLevel := slog.LevelInfo
	if cfg.IsLocal() {
		logLevel = slog.LevelDebug
	}
	logger := logging.New(logging.Options{Local: cfg.IsLocal(), Level: logLevel})
	slog.SetDefault(logger)

	// Build the handler and start serving immediately, so liveness (/healthz) is
	// up before dependencies initialize. /readyz stays not-ready until MarkReady.
	srvHandler := server.New(server.Options{Logger: logger})
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srvHandler,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	// Cloud Run sends SIGTERM to drain; also handle SIGINT for local Ctrl-C. The
	// context cancels on either signal, which also aborts dependency init mid-start.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", slog.String(attrAddr, srv.Addr), slog.String(attrEnv, cfg.Env.String()))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Initialize dependencies with bounded retry; the listener (and /healthz) is
	// already up, so a slow database doesn't look like a dead container.
	pool, dataStore, err := initStore(ctx, logger, cfg.DatabaseURL)
	if err != nil {
		shutdownServer(srv, logger)
		return fmt.Errorf("initialize dependencies: %w", err)
	}
	defer pool.Close()

	srvHandler.MarkReady(dataStore)
	logger.Info("database connected; serving")

	select {
	case err := <-errCh:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		logger.Info("server shutting down")
	}

	shutdownServer(srv, logger)
	return nil
}

// initStore opens the Postgres pool and verifies it, retrying transient failures
// with bounded exponential backoff. A returned store is ready to use; an error
// means the database stayed unreachable, and the caller should give up.
func initStore(ctx context.Context, logger *slog.Logger, databaseURL string) (*pgxpool.Pool, *pgstore.Store, error) {
	pool, err := postgres.New(ctx, postgres.Options{DatabaseURL: databaseURL})
	if err != nil {
		return nil, nil, fmt.Errorf("open database pool: %w", err)
	}

	delay := dbInitBaseDelay
	for attempt := 1; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, dbPingTimeout)
		dataStore, err := pgstore.New(pingCtx, pool)
		cancel()
		if err == nil {
			return pool, dataStore, nil
		}
		if attempt >= dbInitAttempts {
			pool.Close()
			return nil, nil, fmt.Errorf("database unreachable after %d attempts: %w", dbInitAttempts, err)
		}
		logger.Warn("database not ready; retrying",
			slog.Any(attrError, err), slog.Int("attempt", attempt), slog.Duration("retry_in", delay))
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			pool.Close()
			return nil, nil, ctx.Err()
		}
		delay = min(delay*2, dbInitMaxDelay)
	}
}

// shutdownServer drains in-flight requests within shutdownTimeout.
func shutdownServer(srv *http.Server, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", slog.Any(attrError, err))
	}
}
