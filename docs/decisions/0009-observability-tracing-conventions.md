# 0009 — Observability: tracing + structured-logging conventions

**Status:** Accepted (2026-06-23)

Phase 7.5 wires OpenTelemetry tracing and per-request structured logging through the
service. The mechanics are clear from the code (`internal/observability`, the
middleware/interceptors, the manual spans); this record fixes the **conventions** so
new code adds spans the same way without re-deciding, and so reviewers can approve the
pattern once rather than reading each span site.

## Context

The product story (PLAN §"Observability") is "make a request's full story selectively
feedable": one structured log line per request and a span tree per reschedule,
correlated by request id and trace id, so a staging issue can be reconstructed from a
bounded slice. The risk is the opposite of too little instrumentation — it is dozens
of near-identical, hand-varied span sites that are tedious to review and easy to let
drift (a key spelled `lab_id` here and `labId` there breaks every dashboard filter).

## Decision

**One small package owns the surface.** `internal/observability` holds the SDK setup
(`Init`), the span helpers (`Start`/`End`), and the typed attribute constructors. The
pure engine (`internal/dynamicqueue`) never imports it — it stays dependency-free
(ALGORITHM §10); its span is opened one layer up, around the call.

**The uniform span idiom (the thing reviewers approve once):**

```go
func (s *service) do(ctx context.Context, ...) (res Result, err error) {
    ctx, span := observability.Start(ctx, "scheduling.do", observability.SlotID(id))
    defer observability.End(span, &err) // records the error path; no per-return branch
    ...
}
```

`End(span, &err)` takes a pointer to a **named `err` return**, so a failure is recorded
as the span's error status without an `if err != nil` at every return. Every manual
span site looks exactly like this.

**Naming.** Span names are `<layer>.<snake_event>`: `scheduling.clock_in`,
`engine.reschedule`, `store.with_pool`. Span attribute keys are `qlab.`-namespaced
(OTel etiquette — never collide with semantic-convention keys) and produced **only**
through the typed constructors (`LabID`, `PoolID`, `SlotID`, `Event`, `Recommitted`,
`Count`), so a key is never hand-spelled. The bare key strings (`lab_id`, …) are the
single source of truth in that package and are reused verbatim as slog field names, so
a span attribute and its log field read identically when grepping.

**Where spans come from.** The handler/RPC span is automatic — an `otelconnect`
interceptor on both the public API and the operator surface, plus an `otelhttp`
middleware on the router (health probes filtered out). So there is **no per-handler
tracing code**. Manual spans exist at exactly the layers that do work: the scheduling
event (one site in `mutate` covers all seven mutating events, plus `list_slots` and
`poke`), `engine.reschedule` (wrapping the pure engine call), and `store.with_pool`
(tagged with slot/outbox counts). The event span is opened **inside** `mutate`, so a
request rejected at the authz/validation gate before it proceeds is recorded on the
RPC span, not a half-formed event span.

**Structured logging.** `trace_id` is added to every log line once, in
`httpmw.RequestLogger`. The auth interceptor enriches the request-scoped logger with
`lab_id`/`user_id` once the principal is known and emits one line per RPC — again, no
per-handler code.

**Engine output goes to logs, not the span.** A reschedule's per-slot outcome (which
slots got which start/resource, which were re-committed) is a variable-length list, so
it logs (one line in the scheduling layer: the moved slots at `Info`, the full
placement list at `Debug`), correlated by `trace_id`. The `engine.reschedule` span
keeps only the **counts** (`recommitted_count`, `input_slots`). Rationale: span
attributes are for low-cardinality, queryable dimensions you filter/aggregate traces
by; a per-slot list would bloat trace storage, isn't usefully queryable, and works
against the "filter a request's log lines and feed Claude a bounded slice" model —
which is a logs story.

**Exporters, by environment.** `QLAB_ENV` drives the exporter: a stdout exporter
locally (so a span tree is visible immediately) and Google Cloud Trace in
staging/prod (project auto-detected from the Cloud Run service account). Tracing is
never load-bearing — an exporter that fails to build installs a no-op provider and the
service still boots. Sampling is `AlwaysSample`: at ~15 users it is cheap, stays within
the Cloud Trace free tier, and means a reported issue always has its trace. Revisit
(a parent-based ratio) only if volume ever grows.

## Consequences

- New tracing is a copy of the idiom above — minimal review surface, no key drift.
- Authorization/validation failures are visible on the RPC span (with the Connect
  code), not on a scheduling-event span; that is intentional (the event did not run).
- The Cloud Trace export path is unverifiable locally (no cloud from this machine);
  it compiles and is unit-tested via the stdout exporter, and is confirmed in staging
  when the next deploy lands. Metrics are deliberately deferred (PLAN marks them
  optional); add them later behind the same package when a dashboard needs one.
