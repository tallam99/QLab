// Package logging constructs the application's slog.Logger.
//
// Format is environment-driven: human-readable text locally, structured JSON in
// the cloud (where logs are ingested and queried). A request id is attached
// per-request by the httplog middleware, not here.
package logging

import (
	"log/slog"
	"os"
)

// New returns the root logger. When local is true it uses a text handler for
// readability; otherwise a JSON handler for machine ingestion (Cloud Logging).
func New(local bool) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}

	var h slog.Handler
	if local {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h)
}
