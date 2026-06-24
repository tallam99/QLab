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

	firebaseclient "github.com/tallam99/qlab/backend/internal/clients/firebase"
	"github.com/tallam99/qlab/backend/internal/clients/postgres"
	"github.com/tallam99/qlab/backend/internal/config"
	"github.com/tallam99/qlab/backend/internal/devapi"
	"github.com/tallam99/qlab/backend/internal/dynamicqueue"
	slogging "github.com/tallam99/qlab/backend/internal/logging/slog"
	"github.com/tallam99/qlab/backend/internal/server"
	operatorv1 "github.com/tallam99/qlab/backend/internal/services/operator/v1"
	"github.com/tallam99/qlab/backend/internal/store/pgstore"
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

	// Cloud Run sends SIGTERM to drain a container; also handle SIGINT for local
	// Ctrl-C. Cancelling ctx tells the server to shut down (and aborts init).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Build the Firebase Auth client (token verification + the operator token
	// minter). Locally it is pointed at the Auth emulator via
	// FIREBASE_AUTH_EMULATOR_HOST; in staging/prod it uses Application Default
	// Credentials (the Cloud Run service account).
	firebaseAuth, err := firebaseclient.New(ctx, firebaseclient.Options{ProjectID: cfg.FirebaseProjectID})
	if err != nil {
		logger.Error("build firebase client", "error", err)
		return err
	}
	if firebaseclient.UsingEmulator() {
		logger.Warn("Firebase Auth emulator in use — token signatures are NOT verified")
	}

	opts := server.Options{
		Logger:         logger,
		Addr:           ":" + cfg.Port,
		AllowedOrigins: cfg.AllowedOrigins,
		FirebaseAuth:   firebaseAuth,
		Production:     cfg.Env == config.EnvProduction,
	}

	// The operator surface (qlab.dev.v1) exists only outside production (decision
	// 0008). It runs over an elevated, cross-tenant DB connection; main owns that
	// pool and closes it on shutdown.
	if cfg.OperatorEnabled() {
		operatorPool, err := postgres.New(ctx, postgres.Options{DatabaseURL: cfg.OperatorDatabaseURL})
		if err != nil {
			logger.Error("build operator db pool", "error", err)
			return err
		}
		defer operatorPool.Close()
		operatorStore, err := pgstore.New(ctx, operatorPool)
		if err != nil {
			logger.Error("connect operator db", "error", err)
			return err
		}
		minter := firebaseclient.NewMinter(firebaseAuth, cfg.FirebaseAuthEmulatorHost, cfg.FirebaseWebAPIKey)
		operatorSvc := operatorv1.New(operatorv1.Options{Store: operatorStore, Minter: minter})
		path, handler := devapi.New(operatorSvc, cfg.OperatorSecret).Handler()
		opts.OperatorMount = &server.OperatorMount{Path: path, Handler: handler}
		logger.Warn("operator surface (qlab.dev.v1) enabled — staging/local only")
	}

	s := server.New(opts)
	s.InjectDependency(server.WithPostgres(cfg.DatabaseURL))
	// Authentication and scheduling both depend on the store, so register them after
	// WithPostgres. Clock is nil → the scheduling service uses the real time.Now.
	s.InjectDependency(server.WithAuthentication())
	s.InjectDependency(server.WithSchedulingService(dynamicqueue.Minutes(cfg.ClockInGraceMinutes), nil))

	if err := s.Run(ctx); err != nil {
		logger.Error("run server", "error", err)
		return err
	}
	return nil
}
