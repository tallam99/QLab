//go:build integration

package integrationtest

import (
	"context"

	"connectrpc.com/connect"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
)

// spansSince returns the spans recorded after marker (a prior len(traceRecorder.Ended()))
// indexed by name — the spans a single operation produced. Names are unique per op here.
func spansSince(marker int) map[string]sdktrace.ReadOnlySpan {
	ended := traceRecorder.Ended()
	byName := map[string]sdktrace.ReadOnlySpan{}
	for _, sp := range ended[marker:] {
		byName[sp.Name()] = sp
	}
	return byName
}

// TestTracingSpanTree is the PLAN §7.5 exit criterion exercised end to end: a single
// reschedule (a CreateSlot driven through the real server) produces the span tree
// handler -> scheduling.<event> -> {engine.reschedule, store.with_pool}, carrying the
// lab/pool/event annotations.
//
// engine.reschedule and store.with_pool are siblings under the event span (both run
// within the event's orchestration; the engine computes on the rows the tx locked),
// rather than engine nested inside tx — the WithPool callback carries no span context.
func (s *IntegrationSuite) TestTracingSpanTree() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 1)
	// Pre-mint the caller's token (an uninstrumented emulator call) before the marker,
	// so only the CreateSlot tree falls in the window.
	c := h.client(t, lab.Member1, lab.LabID)

	marker := len(traceRecorder.Ended())
	_, err := c.CreateSlot(ctx, createReq(lab.PoolID, at(60), 0, 60, "traced"))
	s.Require().NoError(err)

	byName := spansSince(marker)
	rpc := byName["qlab.v1.QlabService/CreateSlot"]
	event := byName["scheduling.create_slot"]
	engine := byName["engine.reschedule"]
	tx := byName["store.with_pool"]
	s.Require().NotNil(rpc, "otelconnect RPC span (the automatic handler node) is missing")
	s.Require().NotNil(event, "scheduling.create_slot span is missing")
	s.Require().NotNil(engine, "engine.reschedule span is missing")
	s.Require().NotNil(tx, "store.with_pool span is missing")

	// The tree nests correctly: event under the RPC span; engine and tx under the event.
	s.Equal(rpc.SpanContext().SpanID(), event.Parent().SpanID(), "event span should be a child of the RPC span")
	s.Equal(event.SpanContext().SpanID(), engine.Parent().SpanID(), "engine span should be a child of the event span")
	s.Equal(event.SpanContext().SpanID(), tx.Parent().SpanID(), "tx span should be a child of the event span")
	s.Equal(rpc.SpanContext().TraceID(), tx.SpanContext().TraceID(), "the whole tree shares one trace id")

	// The event span carries the agreed annotations.
	s.True(hasAttr(event, "qlab.event", attribute.StringValue("create_slot")))
	s.True(hasAttr(event, "qlab.lab_id", attribute.StringValue(lab.LabID)))
	s.True(hasAttr(event, "qlab.resource_pool_id", attribute.StringValue(lab.PoolID)))
	// The engine span records how many committed starts changed; the tx span, how many
	// rows it wrote — the reschedule's "which starts changed" and write counts.
	s.True(hasAttrKey(engine, "qlab.recommitted_count"))
	s.True(hasAttrKey(tx, "qlab.slots_upserted"))
}

// TestTracingErrorRecorded asserts the deferred End(span, &err) idiom records a failed
// event end to end: an error raised inside the transactional core (clocking in a slot
// that is already ACTIVE → ErrInvalidState, from the build callback) flips both the
// event span and the tx span to the error status. (Pre-mutate failures like an authz
// denial are recorded on the otelconnect RPC span instead, since the event never
// proceeds past the gate — the event span is opened inside mutate by design.)
func (s *IntegrationSuite) TestTracingErrorRecorded() {
	t := s.T()
	ctx := context.Background()
	lab := h.makeLab(t, 1)
	// An already-ACTIVE slot owned by Member1: clocking it in again must fail inside the
	// mutate callback (status is not SCHEDULED), after the event/tx spans are open.
	slotID := h.seedSlot(t, slotSpec{
		Lab: lab.LabID, User: lab.Member1, Pool: lab.PoolID, Resource: lab.Res[0],
		Priority: 1, Status: "ACTIVE", Desired: at(0), Committed: at(0), Actual: at(0), DurationMin: 60,
	})

	c := h.client(t, lab.Member1, lab.LabID)
	marker := len(traceRecorder.Ended())
	_, err := c.ClockIn(ctx, connect.NewRequest(&v1.ClockInRequest{SlotId: slotID}))
	s.Require().Error(err)
	s.Equal(connect.CodeFailedPrecondition, connect.CodeOf(err))

	byName := spansSince(marker)
	event := byName["scheduling.clock_in"]
	tx := byName["store.with_pool"]
	s.Require().NotNil(event, "scheduling.clock_in span is missing")
	s.Require().NotNil(tx, "store.with_pool span is missing")
	// Both spans on the path record the error via their deferred End.
	s.Equal("Error", event.Status().Code.String())
	s.Equal("Error", tx.Status().Code.String())
	s.NotEmpty(event.Events(), "the error should be recorded as a span event")
}

// hasAttr reports whether span carries key with exactly val.
func hasAttr(span sdktrace.ReadOnlySpan, key string, val attribute.Value) bool {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value == val
		}
	}
	return false
}

// hasAttrKey reports whether span carries key with any value.
func hasAttrKey(span sdktrace.ReadOnlySpan, key string) bool {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return true
		}
	}
	return false
}
