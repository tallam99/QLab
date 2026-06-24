//go:build testunit

// These tests pin the reschedule outcome log: the slots whose committed start moved
// log at Info (the meaningful changes, which also fired notifications), the full
// placement list logs at Debug, and an empty outcome logs nothing — so a request's
// story stays reconstructable from logs without re-running the engine, at a volume
// that won't drown prod.
package v1

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/logging"
	"github.com/tallam99/qlab/backend/internal/services/scheduling"
)

// capturedLine is one emitted log line with its level, message, and flattened attrs
// (the With-chain attrs prepended to the call args).
type capturedLine struct {
	level string
	msg   string
	args  []any
}

// captureLogger is a logging.Logger that records every line for assertions.
type captureLogger struct {
	lines *[]capturedLine
	attrs []any
}

func (c captureLogger) emit(level, msg string, args ...any) {
	*c.lines = append(*c.lines, capturedLine{level, msg, append(append([]any{}, c.attrs...), args...)})
}
func (c captureLogger) Debug(msg string, args ...any) { c.emit("debug", msg, args...) }
func (c captureLogger) Info(msg string, args ...any)  { c.emit("info", msg, args...) }
func (c captureLogger) Warn(msg string, args ...any)  { c.emit("warn", msg, args...) }
func (c captureLogger) Error(msg string, args ...any) { c.emit("error", msg, args...) }
func (c captureLogger) With(args ...any) logging.Logger {
	return captureLogger{lines: c.lines, attrs: append(append([]any{}, c.attrs...), args...)}
}

var _ logging.Logger = captureLogger{}

// lineWith returns the first captured line at level whose message is msg, or fails.
func lineWith(t *testing.T, lines []capturedLine, level, msg string) capturedLine {
	t.Helper()
	for _, l := range lines {
		if l.level == level && l.msg == msg {
			return l
		}
	}
	t.Fatalf("no %s line %q in %+v", level, msg, lines)
	return capturedLine{}
}

// hasArg reports whether key (with the given value) appears in a line's alternating
// key/value args.
func hasArg(args []any, key string, want any) bool {
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == key && args[i+1] == want {
			return true
		}
	}
	return false
}

func TestLogReschedule(t *testing.T) {
	lab, pool := uuid.New(), uuid.New()
	moved := scheduling.Position{SlotID: uuid.New(), ActualStart: t0, AssignedResourceID: uuid.New(), Recommitted: true}
	stay := scheduling.Position{SlotID: uuid.New(), ActualStart: t0, AssignedResourceID: uuid.New(), Recommitted: false}

	t.Run("recommitted slots log Info + Debug, tagged with event/lab/pool", func(t *testing.T) {
		var lines []capturedLine
		s := &service{logger: captureLogger{lines: &lines}}
		s.logReschedule(context.Background(), "clock_out", lab, pool, scheduling.Result{Positions: []scheduling.Position{moved, stay}})

		info := lineWith(t, lines, "info", "reschedule moved slots")
		require.True(t, hasArg(info.args, "recommitted_count", 1), "Info logs only the moved slots")
		require.True(t, hasArg(info.args, "event", "clock_out"))
		require.True(t, hasArg(info.args, "lab_id", lab.String()))
		require.True(t, hasArg(info.args, "resource_pool_id", pool.String()))

		debug := lineWith(t, lines, "debug", "reschedule placements")
		require.True(t, hasArg(debug.args, "count", 2), "Debug logs the full placement list")
	})

	t.Run("no recommitted slots: Debug only, no Info", func(t *testing.T) {
		var lines []capturedLine
		s := &service{logger: captureLogger{lines: &lines}}
		s.logReschedule(context.Background(), "create_slot", lab, pool, scheduling.Result{Positions: []scheduling.Position{stay}})

		for _, l := range lines {
			require.NotEqual(t, "info", l.level, "nothing moved, so no Info line")
		}
		lineWith(t, lines, "debug", "reschedule placements")
	})

	t.Run("empty outcome logs nothing", func(t *testing.T) {
		var lines []capturedLine
		s := &service{logger: captureLogger{lines: &lines}}
		s.logReschedule(context.Background(), "cancel", lab, pool, scheduling.Result{})
		require.Empty(t, lines)
	})
}
