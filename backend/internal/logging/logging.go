// Package logging constructs the application's slog.Logger.
//
// Format is environment-driven: human-readable text locally, structured JSON in
// the cloud (where logs are ingested and queried). A request id is attached
// per-request by the httpmw middleware, not here.
package logging

import (
	"log/slog"
	"os"
)

// Options configures New. It is a struct (rather than positional params) so the
// logger's construction can grow new knobs without churning call sites — the
// project convention for constructors.
type Options struct {
	// Local selects a human-readable text handler; otherwise a JSON handler for
	// machine ingestion (Cloud Logging).
	Local bool
	// Level is the minimum level to emit. The zero value (slog.LevelInfo) is the
	// intended default.
	Level slog.Level
}

// New returns the root logger configured per opts.
func New(opts Options) *slog.Logger {
	handlerOpts := &slog.HandlerOptions{Level: opts.Level}

	var h slog.Handler
	if opts.Local {
		h = slog.NewTextHandler(os.Stdout, handlerOpts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, handlerOpts)
	}
	return slog.New(h)
}
