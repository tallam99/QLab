// Command server is the QLab data API entrypoint.
//
// It stays thin by design: load config from the environment, build the logger,
// wire the HTTP handler, and serve with graceful shutdown. All logic lives in
// internal/.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	// databaseConnectTimeout bounds the boot-time connect + ping so a missing
	// database fails the process fast instead of hanging startup.
	databaseConnectTimeout = 10 * time.Second
)

// Log attribute keys (reused across log sites, so kept as consts for a single
// spelling). One-off log *messages* stay inline below.
const (
	attrError = "error"
	attrAddr  = "addr"
	attrEnv   = "env"
)

func main() {
	// Bootstrap logger for the pre-config window: JSON so an early config-load
	// failure is still structured in Cloud Logging (the environment isn't known
	// yet, so we can't pick text-vs-JSON by it). Replaced below once config loads.
	slog.SetDefault(logging.New(logging.Options{Local: false, Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", slog.Any(attrError, err))
		os.Exit(1)
	}

	// Verbose locally, info and above in the cloud.
	logLevel := slog.LevelInfo
	if cfg.IsLocal() {
		logLevel = slog.LevelDebug
	}
	logger := logging.New(logging.Options{Local: cfg.IsLocal(), Level: logLevel})
	slog.SetDefault(logger)

	// Build the pool, then construct the store — which pings the database — before
	// serving, so a misconfigured or unreachable database is a clear boot failure,
	// not a stream of failing requests, and the store handed to the server is
	// already health-checked.
	dbCtx, dbCancel := context.WithTimeout(context.Background(), databaseConnectTimeout)
	pool, err := postgres.New(dbCtx, postgres.Options{DatabaseURL: cfg.DatabaseURL})
	if err != nil {
		dbCancel()
		logger.Error("open database pool", slog.Any(attrError, err))
		os.Exit(1)
	}
	defer pool.Close()

	dataStore, err := pgstore.New(dbCtx, pool)
	dbCancel()
	if err != nil {
		logger.Error("connect database", slog.Any(attrError, err))
		os.Exit(1)
	}
	logger.Info("database connected")

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.New(server.Options{Logger: logger, Readiness: dataStore}),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	// Cloud Run sends SIGTERM to drain a container; also handle SIGINT for local
	// Ctrl-C. NotifyContext cancels ctx on either signal.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", slog.String(attrAddr, srv.Addr), slog.String(attrEnv, cfg.Env.String()))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		logger.Error("server failed", slog.Any(attrError, err))
		os.Exit(1)
	case <-ctx.Done():
		logger.Info("server shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.Any(attrError, err))
		os.Exit(1)
	}
}
