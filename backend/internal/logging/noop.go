package logging

// Noop returns a Logger that discards everything. Useful as a default and in
// tests that don't care about log output.
func Noop() Logger { return noopLogger{} }

type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}
func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}
func (noopLogger) With(...any) Logger   { return noopLogger{} }
