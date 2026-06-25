// Package api implements the QLab Connect-RPC service (the qlab.v1.QlabService
// contract). It is a thin transport adapter: each method authenticates the caller,
// validates+converts the request from proto to the domain, calls the scheduling
// service (which owns the engine + store orchestration), and converts the result
// back. proto types live only in this package (ALGORITHM §10).
//
// Request validation is a separate layer: the protovalidate interceptor enforces
// the .proto rules (uuid ids, positive durations, a desired start) before any
// handler runs, so handlers assume structurally valid input. The scheduling
// service is attached after the database is ready (SetScheduling), so it is held
// behind an atomic pointer: until set, methods return CodeUnavailable.
package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"connectrpc.com/validate"
	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/principal"
	"github.com/tallam99/qlab/backend/internal/protoconv"
	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
	"github.com/tallam99/qlab/backend/internal/protogen/qlab/v1/qlabv1connect"
	"github.com/tallam99/qlab/backend/internal/realtime"
	"github.com/tallam99/qlab/backend/internal/services/authentication"
	"github.com/tallam99/qlab/backend/internal/services/scheduling"
)

// Service implements qlabv1connect.QlabServiceHandler.
type Service struct {
	qlabv1connect.UnimplementedQlabServiceHandler
	// sched holds the scheduling service, attached once the store is ready. A
	// pointer-to-interface in an atomic.Pointer so reads are race-free (the server
	// serves before dependencies are injected) and a pre-readiness call sees nil.
	sched atomic.Pointer[scheduling.Service]
	// authn holds the authentication service (token -> user), attached once the store
	// is ready, like sched. The auth interceptor reads it per call; nil => Unavailable.
	authn atomic.Pointer[authentication.Service]
	// broker fans pool-schedule changes out to the SSE stream handler, attached once
	// the realtime stack is ready (SetBroker). Read through an atomic pointer like the
	// others; nil => the stream endpoint returns Unavailable.
	broker atomic.Pointer[realtime.Broker]
	// validate enforces the .proto buf.validate rules at the transport edge.
	validate connect.Interceptor
	// otel opens an RPC span (named for the procedure) and propagates trace context on
	// every call — the automatic "handler" node of the span tree, so no per-method code.
	otel connect.Interceptor
}

// New builds the service and its transport interceptors: an OpenTelemetry interceptor
// (per-RPC span + trace propagation) and the request-validation interceptor (the
// .proto buf.validate rules). A failure building the otel interceptor is a wiring bug
// (bad provider config), so it panics rather than returns an error.
func New() *Service {
	otelInterceptor, err := otelconnect.NewInterceptor()
	if err != nil {
		panic(fmt.Sprintf("api: build otelconnect interceptor: %v", err))
	}
	return &Service{validate: validate.NewInterceptor(), otel: otelInterceptor}
}

// SetScheduling attaches the scheduling service. Called by the server's dependency
// injector once the store is ready, before the server marks itself ready.
func (s *Service) SetScheduling(svc scheduling.Service) {
	s.sched.Store(&svc)
}

// SetAuthentication attaches the authentication service the auth interceptor uses.
// Called by the server's dependency injector once the store is ready.
func (s *Service) SetAuthentication(svc authentication.Service) {
	s.authn.Store(&svc)
}

// SetBroker attaches the realtime broker the SSE stream handler subscribes to.
// Called by the server's dependency injector once the realtime stack is ready.
func (s *Service) SetBroker(broker *realtime.Broker) {
	s.broker.Store(broker)
}

// Handler returns the mount path and the HTTP handler for the Connect service. Three
// interceptors run before every method, outermost first: OpenTelemetry (open the RPC
// span so it wraps everything below, including auth/validation failures), then
// authentication (reject or attach the principal), then protovalidate (enforce the
// .proto rules). So a handler runs only for an authenticated caller with a
// structurally valid request, and every call is traced.
func (s *Service) Handler() (string, http.Handler) {
	return qlabv1connect.NewQlabServiceHandler(s, connect.WithInterceptors(s.otel, s.authInterceptor(), s.validate))
}

// caller resolves the authenticated principal and the ready scheduling service, or
// the Connect error a handler should surface (Unauthenticated when no principal,
// Unavailable before the service is wired).
func (s *Service) caller(ctx context.Context) (principal.Principal, scheduling.Service, error) {
	p, ok := principal.FromContext(ctx)
	if !ok {
		return principal.Principal{}, nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	svc := s.sched.Load()
	if svc == nil {
		return principal.Principal{}, nil, connect.NewError(connect.CodeUnavailable, errors.New("service not ready"))
	}
	return p, *svc, nil
}

// parseUUID converts a request id (already shape-checked by the validate
// interceptor) to a uuid, surfacing any residual parse failure as InvalidArgument.
func parseUUID(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid id %q: %w", s, err))
	}
	return id, nil
}

// ListSlots returns the pool's slots for the caller's lab.
func (s *Service) ListSlots(ctx context.Context, req *connect.Request[v1.ListSlotsRequest]) (*connect.Response[v1.ListSlotsResponse], error) {
	p, svc, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	poolID, err := parseUUID(req.Msg.GetResourcePoolId())
	if err != nil {
		return nil, err
	}
	slots, err := svc.ListSlots(ctx, p, poolID)
	if err != nil {
		return nil, connectError(err)
	}
	out := &v1.ListSlotsResponse{Slots: make([]*v1.Slot, 0, len(slots))}
	for _, sl := range slots {
		out.Slots = append(out.Slots, protoconv.Slot(sl))
	}
	return connect.NewResponse(out), nil
}

// GetSchedule returns the pool's current schedule (the engine run read-only against
// now) for the caller's lab — the read the UI loads a pool's queue from. Mutates
// nothing.
func (s *Service) GetSchedule(ctx context.Context, req *connect.Request[v1.GetScheduleRequest]) (*connect.Response[v1.GetScheduleResponse], error) {
	p, svc, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	poolID, err := parseUUID(req.Msg.GetResourcePoolId())
	if err != nil {
		return nil, err
	}
	result, err := svc.Schedule(ctx, p, poolID)
	if err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.GetScheduleResponse{Result: resultToProto(result)}), nil
}

// CreateSlot books a slot for the caller and returns the recomputed schedule.
func (s *Service) CreateSlot(ctx context.Context, req *connect.Request[v1.CreateSlotRequest]) (*connect.Response[v1.CreateSlotResponse], error) {
	p, svc, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	poolID, err := parseUUID(req.Msg.GetResourcePoolId())
	if err != nil {
		return nil, err
	}
	result, err := svc.CreateSlot(ctx, p, scheduling.CreateParams{
		ResourcePoolID:   poolID,
		DesiredStart:     timeFromProto(req.Msg.GetDesiredStart()),
		LookaheadMinutes: req.Msg.GetLookaheadMinutes(),
		DurationMinutes:  req.Msg.GetDurationMinutes(),
		Note:             req.Msg.GetNote(),
	})
	if err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.CreateSlotResponse{Result: resultToProto(result)}), nil
}

// ClockIn marks the caller's slot active.
func (s *Service) ClockIn(ctx context.Context, req *connect.Request[v1.ClockInRequest]) (*connect.Response[v1.ClockInResponse], error) {
	p, svc, slotID, err := s.callerWithSlot(ctx, req.Msg.GetSlotId())
	if err != nil {
		return nil, err
	}
	result, err := svc.ClockUserIn(ctx, p, slotID)
	if err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.ClockInResponse{Result: resultToProto(result)}), nil
}

// ClockOut settles the caller's active slot (clock-out / early finish).
func (s *Service) ClockOut(ctx context.Context, req *connect.Request[v1.ClockOutRequest]) (*connect.Response[v1.ClockOutResponse], error) {
	p, svc, slotID, err := s.callerWithSlot(ctx, req.Msg.GetSlotId())
	if err != nil {
		return nil, err
	}
	result, err := svc.ClockUserOut(ctx, p, slotID)
	if err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.ClockOutResponse{Result: resultToProto(result)}), nil
}

// CancelSlot cancels the caller's slot.
func (s *Service) CancelSlot(ctx context.Context, req *connect.Request[v1.CancelSlotRequest]) (*connect.Response[v1.CancelSlotResponse], error) {
	p, svc, slotID, err := s.callerWithSlot(ctx, req.Msg.GetSlotId())
	if err != nil {
		return nil, err
	}
	result, err := svc.CancelSlot(ctx, p, slotID)
	if err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.CancelSlotResponse{Result: resultToProto(result)}), nil
}

// PokeOccupant nudges an overrunning occupant; it changes no schedule state.
func (s *Service) PokeOccupant(ctx context.Context, req *connect.Request[v1.PokeOccupantRequest]) (*connect.Response[v1.PokeOccupantResponse], error) {
	p, svc, slotID, err := s.callerWithSlot(ctx, req.Msg.GetSlotId())
	if err != nil {
		return nil, err
	}
	if err := svc.PokeOccupant(ctx, p, slotID); err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.PokeOccupantResponse{}), nil
}

// ForceClockOut boots an overrunning occupant and returns the recomputed schedule.
func (s *Service) ForceClockOut(ctx context.Context, req *connect.Request[v1.ForceClockOutRequest]) (*connect.Response[v1.ForceClockOutResponse], error) {
	p, svc, slotID, err := s.callerWithSlot(ctx, req.Msg.GetSlotId())
	if err != nil {
		return nil, err
	}
	result, err := svc.ForceClockUserOut(ctx, p, slotID)
	if err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.ForceClockOutResponse{Result: resultToProto(result)}), nil
}

// ForceNoShow reclaims a grace-lapsed slot and returns the recomputed schedule.
func (s *Service) ForceNoShow(ctx context.Context, req *connect.Request[v1.ForceNoShowRequest]) (*connect.Response[v1.ForceNoShowResponse], error) {
	p, svc, slotID, err := s.callerWithSlot(ctx, req.Msg.GetSlotId())
	if err != nil {
		return nil, err
	}
	result, err := svc.ForceUserNoShow(ctx, p, slotID)
	if err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.ForceNoShowResponse{Result: resultToProto(result)}), nil
}

// callerWithSlot resolves the caller, the ready service, and the parsed slot id —
// the common preamble of the slot-targeting RPCs.
func (s *Service) callerWithSlot(ctx context.Context, rawSlotID string) (principal.Principal, scheduling.Service, uuid.UUID, error) {
	p, svc, err := s.caller(ctx)
	if err != nil {
		return principal.Principal{}, nil, uuid.Nil, err
	}
	slotID, err := parseUUID(rawSlotID)
	if err != nil {
		return principal.Principal{}, nil, uuid.Nil, err
	}
	return p, svc, slotID, nil
}

// connectError maps a scheduling domain error to a Connect status code.
func connectError(err error) error {
	switch {
	case errors.Is(err, scheduling.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, scheduling.ErrNotMember), errors.Is(err, scheduling.ErrForbidden):
		return connect.NewError(connect.CodePermissionDenied, err)
	case errors.Is(err, scheduling.ErrInvalidState):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	default:
		// An unexpected error (DB failure, engine bug): surface as Internal without
		// leaking internals beyond the wrapped message.
		return connect.NewError(connect.CodeInternal, err)
	}
}
