// Package observability holds the service's tracing surface: the OpenTelemetry SDK
// setup (Init) and a small, uniform span API (Start/End + typed attribute
// constructors) so every call site reads identically and attribute names can't
// drift between spans and log lines.
//
// The handler/RPC span is created automatically by the otelconnect interceptor and
// the otelhttp middleware; this package is what the deeper layers (scheduling,
// engine wrapper, store transaction) use to add child spans. The pure scheduling
// engine (internal/dynamicqueue) deliberately does NOT import this — it stays
// dependency-free (ALGORITHM §10); its span is opened one layer up, around the call.
package observability

import (
	"context"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the instrumentation scope recorded on every span.
const tracerName = "qlab"

// tracer returns the service tracer from the global provider. It is fetched per call
// (not cached) so it picks up whatever provider Init installed — and a no-op provider
// before Init runs, which makes Start/End safe to call unconditionally in tests.
func tracer() trace.Tracer { return otel.Tracer(tracerName) }

// Start opens a child span named name carrying attrs. Always pair it with a deferred
// End. name follows the `<layer>.<snake_event>` convention (e.g. "scheduling.clock_in",
// "engine.reschedule", "store.with_pool").
func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer().Start(ctx, name, trace.WithAttributes(attrs...))
}

// End finishes span, recording *errp (when non-nil) as the span's error status so the
// failure path is captured without a branch at every return. Pass a pointer to a NAMED
// error return value:
//
//	func (s *service) do(ctx context.Context, ...) (res Result, err error) {
//	    ctx, span := observability.Start(ctx, "scheduling.do", observability.SlotID(id))
//	    defer observability.End(span, &err)
//	    ...
//	}
func End(span trace.Span, errp *error) {
	if errp != nil && *errp != nil {
		span.RecordError(*errp)
		span.SetStatus(codes.Error, (*errp).Error())
	}
	span.End()
}

// Annotate adds attrs to the span already in ctx (the current handler/RPC span, say),
// for layers that enrich a span they did not open — e.g. the auth interceptor tagging
// the caller. A no-op when no span is recording.
func Annotate(ctx context.Context, attrs ...attribute.KeyValue) {
	trace.SpanFromContext(ctx).SetAttributes(attrs...)
}

// TraceID returns the active trace id as a hex string for log correlation, or "" when
// no span is recording (so a log line simply omits the field rather than logging zeros).
func TraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}

// Attribute keys. Bare (un-namespaced) spellings, kept here as the single source of
// truth so a span attribute and the matching slog field read identically when
// grepping a request's story. The span constructors below namespace them under
// "qlab." (OTel etiquette: never collide with semantic-convention keys); log sites
// reuse the same bare strings via the exported Key* consts.
const attrNamespace = "qlab."

const (
	KeyLabID        = "lab_id"
	KeyResourcePool = "resource_pool_id"
	KeySlotID       = "slot_id"
	KeyResourceID   = "resource_id"
	KeyUserID       = "user_id"
	KeyEvent        = "event"
	KeyTraceID      = "trace_id"
	// KeyRecommitted counts slots whose committed_start changed in a reschedule —
	// the "which starts changed" annotation PLAN §7.5 calls for.
	keyRecommitted = "recommitted_count"
)

// Typed span-attribute constructors. Call sites use only these, so a key is never
// hand-spelled and a value is never the wrong kind.
func LabID(id uuid.UUID) attribute.KeyValue {
	return attribute.String(attrNamespace+KeyLabID, id.String())
}
func PoolID(id uuid.UUID) attribute.KeyValue {
	return attribute.String(attrNamespace+KeyResourcePool, id.String())
}
func SlotID(id uuid.UUID) attribute.KeyValue {
	return attribute.String(attrNamespace+KeySlotID, id.String())
}
func ResourceID(id uuid.UUID) attribute.KeyValue {
	return attribute.String(attrNamespace+KeyResourceID, id.String())
}
func UserID(id uuid.UUID) attribute.KeyValue {
	return attribute.String(attrNamespace+KeyUserID, id.String())
}
func Event(name string) attribute.KeyValue { return attribute.String(attrNamespace+KeyEvent, name) }
func Recommitted(n int) attribute.KeyValue { return attribute.Int(attrNamespace+keyRecommitted, n) }

// Count tags a span with a named integer (e.g. Count("slots_upserted", n)). The name
// is namespaced like the rest; use it for the ad-hoc counts that don't warrant a
// dedicated constructor.
func Count(name string, n int) attribute.KeyValue { return attribute.Int(attrNamespace+name, n) }
