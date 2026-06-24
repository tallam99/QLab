package httpmw

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// spanName is the operation label otelhttp records before the per-request formatter
// renames each span to "METHOD path".
const spanName = "qlab.http"

// Tracing returns middleware that opens a server span for each request and extracts
// any inbound W3C trace context (so a caller's trace continues through this service).
// It is the sibling of RequestID/RequestLogger in the middleware chain and must run
// above RequestLogger, which reads the span's trace id onto every log line.
//
// exemptPaths (the health/readiness probes) are skipped: they fire constantly and
// carry no useful trace, so excluding them keeps the trace backend free of noise.
func Tracing(exemptPaths ...string) func(http.Handler) http.Handler {
	exempt := make(map[string]bool, len(exemptPaths))
	for _, p := range exemptPaths {
		exempt[p] = true
	}
	return otelhttp.NewMiddleware(spanName,
		otelhttp.WithFilter(func(r *http.Request) bool { return !exempt[r.URL.Path] }),
		// Name each span for its method + path (e.g. "POST /qlab.v1.QlabService/ClockIn")
		// rather than the static operation label, so spans are legible without opening them.
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
}
