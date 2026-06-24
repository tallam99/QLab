package observability

import (
	"context"

	cloudtrace "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Options configures Init.
type Options struct {
	// ServiceName is the resource service.name on every span (e.g. "qlab-api").
	ServiceName string
	// Version is the resource service.version; defaults to "dev" when empty.
	Version string
	// Environment is the QLAB_ENV value, recorded as deployment.environment so traces
	// from staging and prod are distinguishable in Cloud Trace.
	Environment string
	// Local selects the stdout exporter (spans printed locally, per the PLAN §7.5 exit
	// criteria); otherwise spans export to Google Cloud Trace (staging/prod).
	Local bool
}

// Init builds and installs the global TracerProvider and the W3C trace-context
// propagator, returning a shutdown func that flushes pending spans (call it on
// shutdown). Local runs export to stdout synchronously (so a span tree is visible
// immediately); cloud runs batch-export to Google Cloud Trace, whose project is
// auto-detected from the Cloud Run service account's credentials.
//
// Tracing is never load-bearing: if the exporter cannot be built, Init installs a
// no-op provider and returns the error for the caller to log — the service still boots.
func Init(ctx context.Context, opts Options) (func(context.Context) error, error) {
	exporter, err := newExporter(ctx, opts.Local)
	if err != nil {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, err
	}

	// Local: WithSyncer flushes each span as it ends, so stdout shows the tree right
	// away. Cloud: WithBatcher amortizes export calls under real traffic.
	processor := sdktrace.WithBatcher(exporter)
	if opts.Local {
		processor = sdktrace.WithSyncer(exporter)
	}

	tp := sdktrace.NewTracerProvider(
		processor,
		sdktrace.WithResource(newResource(opts)),
		// AlwaysSample: this is a ~15-user service, so full sampling is cheap, stays
		// within the Cloud Trace free tier, and means a reported issue always has its
		// trace. Revisit (ParentBased ratio) only if volume ever grows.
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))
	return tp.Shutdown, nil
}

// newExporter returns the stdout exporter locally and the Cloud Trace exporter in the
// cloud.
func newExporter(ctx context.Context, local bool) (sdktrace.SpanExporter, error) {
	if local {
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	}
	// Project id is left to the exporter's auto-detection (the Cloud Run service
	// account's Application Default Credentials), so no project is hardcoded here.
	return cloudtrace.New()
}

// newResource describes this service on every span: service.name/version and the
// deployment environment. NewSchemaless avoids a schema-URL conflict when merging
// onto resource.Default()'s detected attributes (host, process, SDK).
func newResource(opts Options) *resource.Resource {
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	attrs := resource.NewSchemaless(
		attribute.String("service.name", opts.ServiceName),
		attribute.String("service.version", version),
		attribute.String("deployment.environment", opts.Environment),
	)
	// Merge can only error on a schema-URL mismatch, which NewSchemaless precludes;
	// on the impossible error fall back to just our attributes rather than panicking.
	merged, err := resource.Merge(resource.Default(), attrs)
	if err != nil {
		return attrs
	}
	return merged
}
