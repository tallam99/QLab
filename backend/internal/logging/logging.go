// Package logging constructs the application's logger.
//
// The service depends on the Logger interface (the methods it actually uses), not
// on slog directly, so a test or alternate backend can be swapped in. New returns
// a slog-backed implementation: human-readable text locally, structured JSON in
// the cloud (where logs are ingested and queried). A request id is attached
// per-request by the httpmw middleware, not here.
package logging

import (
	"log/slog"
	"os"
)

// Logger is the logging surface the service uses. Methods mirror slog's
// leveled-logging shape: msg plus alternating key/value args.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	// With returns a Logger that includes the given key/value attributes on every
	// subsequent line.
	With(args ...any) Logger
}

// Options configures New. It is a struct (rather than positional params) so the
// logger's construction can grow new knobs without churning call sites.
type Options struct {
	// Local selects a human-readable text handler; otherwise a JSON handler for
	// machine ingestion (Cloud Logging).
	Local bool
	// Level is the minimum level to emit. The zero value (slog.LevelInfo) is the
	// intended default.
	Level slog.Level
}

// New returns the root logger configured per opts.
func New(opts Options) Logger {
	handlerOpts := &slog.HandlerOptions{Level: opts.Level}

	var h slog.Handler
	if opts.Local {
		h = slog.NewTextHandler(os.Stdout, handlerOpts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, handlerOpts)
	}
	return slogLogger{logger: slog.New(h)}
}

// slogLogger adapts *slog.Logger to Logger.
type slogLogger struct{ logger *slog.Logger }

func (s slogLogger) Debug(msg string, args ...any) { s.logger.Debug(msg, args...) }
func (s slogLogger) Info(msg string, args ...any)  { s.logger.Info(msg, args...) }
func (s slogLogger) Warn(msg string, args ...any)  { s.logger.Warn(msg, args...) }
func (s slogLogger) Error(msg string, args ...any) { s.logger.Error(msg, args...) }
func (s slogLogger) With(args ...any) Logger       { return slogLogger{logger: s.logger.With(args...)} }
