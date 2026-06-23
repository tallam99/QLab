// Command server is the QLab data API entrypoint.
//
// It stays thin: load config, build the logger, construct the server, register
// its dependencies, and hand control to the server's Run loop (which serves,
// initializes dependencies, marks ready, and shuts down). All logic lives in
// internal/.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/tallam99/qlab/backend/internal/config"
	"github.com/tallam99/qlab/backend/internal/dynamicqueue"
	slogging "github.com/tallam99/qlab/backend/internal/logging/slog"
	"github.com/tallam99/qlab/backend/internal/server"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	// Bootstrap logger (JSON) so a failure before config is loaded is still
	// structured in Cloud Logging; replaced once the environment is known.
	boot := slogging.New(slogging.Options{Local: false, Level: slog.LevelInfo})

	cfg, err := config.Load()
	if err != nil {
		boot.Error("load config", "error", err)
		return err
	}

	// Verbose locally, info and above in the cloud; stamp env on every line.
	logLevel := slog.LevelInfo
	if cfg.IsLocal() {
		logLevel = slog.LevelDebug
	}
	logger := slogging.New(slogging.Options{Local: cfg.IsLocal(), Level: logLevel}).
		With("env", cfg.Env.String())

	s := server.New(server.Options{
		Logger:         logger,
		Addr:           ":" + cfg.Port,
		AllowedOrigins: cfg.AllowedOrigins,
	})
	s.InjectDependency(server.WithPostgres(cfg.DatabaseURL))
	// Scheduling depends on the store, so register it after WithPostgres. Clock is
	// nil here → the service uses the real time.Now.
	s.InjectDependency(server.WithSchedulingService(dynamicqueue.Minutes(cfg.ClockInGraceMinutes), nil))

	// Cloud Run sends SIGTERM to drain a container; also handle SIGINT for local
	// Ctrl-C. Cancelling ctx tells the server to shut down (and aborts init).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := s.Run(ctx); err != nil {
		logger.Error("run server", "error", err)
		return err
	}
	return nil
}
