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

	"github.com/tallam99/qlab/backend/internal/config"
	"github.com/tallam99/qlab/backend/internal/logging"
	"github.com/tallam99/qlab/backend/internal/server"
)

const (
	// readHeaderTimeout bounds how long a client may take to send request headers
	// (a cheap slowloris guard).
	readHeaderTimeout = 5 * time.Second
	// shutdownTimeout bounds graceful drain of in-flight requests on SIGTERM.
	shutdownTimeout = 10 * time.Second
)

// Log attribute keys (reused across log sites, so kept as consts for a single
// spelling). One-off log *messages* stay inline below.
const (
	attrError = "error"
	attrAddr  = "addr"
	attrEnv   = "env"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", slog.Any(attrError, err))
		os.Exit(1)
	}

	logger := logging.New(logging.Options{Local: cfg.IsLocal()})
	slog.SetDefault(logger)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.New(server.Options{Logger: logger}),
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
