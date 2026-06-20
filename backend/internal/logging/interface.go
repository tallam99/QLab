// Package logging defines the application's logging interface. The service depends
// on it (the methods it actually uses), not on any backend, so a fake or alternate
// implementation can be swapped in. The slog-backed implementation lives in
// logging/slog.
package logging

// Logger is the logging surface the service uses. Methods mirror leveled logging:
// a message plus alternating key/value args.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	// With returns a Logger that includes the given key/value attributes on every
	// subsequent line.
	With(args ...any) Logger
}
