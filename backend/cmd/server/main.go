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

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", slog.Any("error", err))
		os.Exit(1)
	}

	logger := logging.New(cfg.IsLocal())
	slog.SetDefault(logger)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.New(logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Cloud Run sends SIGTERM to drain a container; also handle SIGINT for local
	// Ctrl-C. NotifyContext cancels ctx on either signal.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", slog.String("addr", srv.Addr), slog.String("env", cfg.Env))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		logger.Error("server failed", slog.Any("error", err))
		os.Exit(1)
	case <-ctx.Done():
		logger.Info("server shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.Any("error", err))
		os.Exit(1)
	}
}
