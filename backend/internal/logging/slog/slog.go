// Package slog is the slog-backed implementation of logging.Logger: human-readable
// text locally, structured JSON in the cloud (where logs are ingested and
// queried). A request id is attached per-request by the httpmw middleware, not here.
package slog

import (
	stdslog "log/slog"
	"os"

	"github.com/tallam99/qlab/backend/internal/logging"
)

// Options configures New. It is a struct (rather than positional params) so the
// logger's construction can grow new knobs without churning call sites. It is
// specific to this implementation — other backends define their own.
type Options struct {
	// Local selects a human-readable text handler; otherwise a JSON handler for
	// machine ingestion (Cloud Logging).
	Local bool
	// Level is the minimum level to emit.
	Level stdslog.Level
}

// New returns a slog-backed logging.Logger configured per opts.
func New(opts Options) logging.Logger {
	handlerOpts := &stdslog.HandlerOptions{Level: opts.Level}

	var h stdslog.Handler
	if opts.Local {
		h = stdslog.NewTextHandler(os.Stdout, handlerOpts)
	} else {
		h = stdslog.NewJSONHandler(os.Stdout, handlerOpts)
	}
	return logger{l: stdslog.New(h)}
}

// logger adapts *slog.Logger to logging.Logger.
type logger struct{ l *stdslog.Logger }

func (s logger) Debug(msg string, args ...any) { s.l.Debug(msg, args...) }
func (s logger) Info(msg string, args ...any)  { s.l.Info(msg, args...) }
func (s logger) Warn(msg string, args ...any)  { s.l.Warn(msg, args...) }
func (s logger) Error(msg string, args ...any) { s.l.Error(msg, args...) }
func (s logger) With(args ...any) logging.Logger {
	return logger{l: s.l.With(args...)}
}
