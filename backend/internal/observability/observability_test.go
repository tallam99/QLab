//go:build testunit

// These tests pin the span helpers' behavior: that End records the error path, that
// the typed attribute constructors emit the agreed `qlab.`-namespaced keys (the
// contract log sites and dashboards depend on), and that TraceID round-trips. They
// use an in-memory recorder rather than asserting against stdout/Cloud Trace.
package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// withRecorder installs a recording tracer provider for the duration of a test and
// returns the recorder so a test can read back the spans it produced.
func withRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return rec
}

// TestEndRecordsError asserts End sets the span's status to Error and records the
// error when (and only when) the error pointer holds a non-nil error — the behavior
// the `defer End(span, &err)` idiom relies on to capture failures without a branch.
func TestEndRecordsError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus codes.Code
		wantEvents int
	}{
		{name: "success leaves status unset", err: nil, wantStatus: codes.Unset, wantEvents: 0},
		{name: "failure sets error status", err: errors.New("boom"), wantStatus: codes.Error, wantEvents: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := withRecorder(t)
			_, span := Start(context.Background(), "test.op", LabID(uuid.New()))
			err := tc.err
			End(span, &err)

			spans := rec.Ended()
			require.Len(t, spans, 1)
			require.Equal(t, "test.op", spans[0].Name())
			require.Equal(t, tc.wantStatus, spans[0].Status().Code)
			require.Len(t, spans[0].Events(), tc.wantEvents)
		})
	}
}

// TestEndNilPointer guards the defensive nil-pointer branch: End must not panic when
// handed a nil error pointer (it still ends the span).
func TestEndNilPointer(t *testing.T) {
	rec := withRecorder(t)
	_, span := Start(context.Background(), "test.op")
	End(span, nil)
	require.Len(t, rec.Ended(), 1)
}

// TestAttributeConstructors pins the exact attribute keys and value kinds: these are
// the contract dashboards and log correlation read, so a rename must break a test.
func TestAttributeConstructors(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name string
		kv   attribute.KeyValue
		key  string
		val  attribute.Value
	}{
		{"lab", LabID(id), "qlab.lab_id", attribute.StringValue(id.String())},
		{"pool", PoolID(id), "qlab.resource_pool_id", attribute.StringValue(id.String())},
		{"slot", SlotID(id), "qlab.slot_id", attribute.StringValue(id.String())},
		{"resource", ResourceID(id), "qlab.resource_id", attribute.StringValue(id.String())},
		{"user", UserID(id), "qlab.user_id", attribute.StringValue(id.String())},
		{"event", Event("clock_in"), "qlab.event", attribute.StringValue("clock_in")},
		{"recommitted", Recommitted(3), "qlab.recommitted_count", attribute.IntValue(3)},
		{"count", Count("slots_upserted", 5), "qlab.slots_upserted", attribute.IntValue(5)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.key, string(tc.kv.Key))
			require.Equal(t, tc.val, tc.kv.Value)
		})
	}
}

// TestAnnotateSetsAttributesOnCurrentSpan asserts Annotate tags the span already in
// context (what the auth interceptor uses), and is a no-op when none is recording.
func TestAnnotateSetsAttributesOnCurrentSpan(t *testing.T) {
	rec := withRecorder(t)
	id := uuid.New()

	ctx, span := Start(context.Background(), "test.op")
	Annotate(ctx, UserID(id))
	span.End()

	require.NotPanics(t, func() { Annotate(context.Background(), LabID(id)) }) // no span in ctx

	spans := rec.Ended()
	require.Len(t, spans, 1)
	require.Contains(t, spans[0].Attributes(), UserID(id))
}

// TestTraceID returns the active trace id inside a recording span and "" outside one,
// so a log line omits the field rather than emitting zeros.
func TestTraceID(t *testing.T) {
	withRecorder(t)
	require.Empty(t, TraceID(context.Background()))

	ctx, span := Start(context.Background(), "test.op")
	defer span.End()
	require.Equal(t, span.SpanContext().TraceID().String(), TraceID(ctx))
	require.Len(t, TraceID(ctx), 32) // 16-byte trace id, hex-encoded
}
