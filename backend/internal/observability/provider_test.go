//go:build testunit

package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

// TestInitLocalInstallsWorkingProvider asserts the local path builds a real provider
// (stdout exporter), installs it globally so Start produces a recording span, and
// returns a shutdown that flushes cleanly. This is the PLAN §7.5 "exported to stdout
// locally" path exercised without scraping stdout.
func TestInitLocalInstallsWorkingProvider(t *testing.T) {
	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	shutdown, err := Init(context.Background(), Options{ServiceName: "qlab-api-test", Environment: "local", Local: true})
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// The installed provider records: a started span carries a real trace id.
	ctx, span := Start(context.Background(), "test.op")
	require.True(t, span.SpanContext().IsValid())
	require.NotEmpty(t, TraceID(ctx))
	span.End()

	require.NoError(t, shutdown(context.Background()))
}

// TestInitDefaultsVersion checks the empty-version default ("dev") so the resource is
// always tagged — the small branch in newResource that mutation testing would probe.
func TestInitDefaultsVersion(t *testing.T) {
	res := newResource(Options{ServiceName: "qlab-api-test", Environment: "local"})
	var version string
	for _, kv := range res.Attributes() {
		if kv.Key == "service.version" {
			version = kv.Value.AsString()
		}
	}
	require.Equal(t, "dev", version)
}

// TestInitExplicitVersion checks an explicit version is carried through unchanged.
func TestInitExplicitVersion(t *testing.T) {
	res := newResource(Options{ServiceName: "qlab-api-test", Environment: "staging", Version: "1.2.3"})
	var version, env string
	for _, kv := range res.Attributes() {
		switch kv.Key {
		case "service.version":
			version = kv.Value.AsString()
		case "deployment.environment":
			env = kv.Value.AsString()
		}
	}
	require.Equal(t, "1.2.3", version)
	require.Equal(t, "staging", env)
}
