// Package api implements the QLab Connect-RPC service (the qlab.v1.QlabService
// contract). It is a thin transport adapter: each method authenticates the caller,
// converts the request from proto to the domain, calls the scheduling service
// (which owns the engine + store orchestration), and converts the result back.
// proto types live only in this package (ALGORITHM §10).
//
// The scheduling service is attached after the database is ready (SetScheduling,
// called by the server's injector), so it is held behind an atomic pointer: until
// it is set, methods return CodeUnavailable, mirroring the readiness gate.
package api

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"

	"connectrpc.com/connect"

	"github.com/tallam99/qlab/backend/internal/principal"
	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
	"github.com/tallam99/qlab/backend/internal/protogen/qlab/v1/qlabv1connect"
	"github.com/tallam99/qlab/backend/internal/scheduling"
)

// Service implements qlabv1connect.QlabServiceHandler. The embedded
// UnimplementedQlabServiceHandler keeps it satisfying the interface even before
// every method is overridden.
type Service struct {
	qlabv1connect.UnimplementedQlabServiceHandler
	// sched holds the scheduling service, attached once the store is ready. A
	// pointer-to-interface in an atomic.Pointer so reads are race-free and a
	// pre-readiness call sees nil rather than a torn value.
	sched atomic.Pointer[scheduling.Service]
}

// New builds the service. The scheduling dependency is attached later via
// SetScheduling (after the database connects), so New takes nothing.
func New() *Service {
	return &Service{}
}

// SetScheduling attaches the scheduling service. Called by the server's dependency
// injector once the store is ready, before the server marks itself ready.
func (s *Service) SetScheduling(svc scheduling.Service) {
	s.sched.Store(&svc)
}

// Handler returns the mount path and the HTTP handler for the Connect service.
func (s *Service) Handler() (string, http.Handler) {
	return qlabv1connect.NewQlabServiceHandler(s)
}

// caller resolves the authenticated principal and the ready scheduling service, or
// returns the Connect error a handler should surface (Unauthenticated when no
// principal, Unavailable before the service is wired).
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

// ListSlots returns the pool's slots for the caller's lab.
func (s *Service) ListSlots(ctx context.Context, req *connect.Request[v1.ListSlotsRequest]) (*connect.Response[v1.ListSlotsResponse], error) {
	p, svc, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	slots, err := svc.ListSlots(ctx, p, req.Msg.GetResourcePoolId())
	if err != nil {
		return nil, connectError(err)
	}
	out := &v1.ListSlotsResponse{Slots: make([]*v1.Slot, 0, len(slots))}
	for _, sl := range slots {
		out.Slots = append(out.Slots, slotToProto(sl))
	}
	return connect.NewResponse(out), nil
}

// CreateSlot books a slot for the caller and returns the recomputed schedule.
func (s *Service) CreateSlot(ctx context.Context, req *connect.Request[v1.CreateSlotRequest]) (*connect.Response[v1.CreateSlotResponse], error) {
	p, svc, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	result, err := svc.CreateSlot(ctx, p, scheduling.CreateParams{
		ResourcePoolID:   req.Msg.GetResourcePoolId(),
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
	p, svc, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	result, err := svc.ClockIn(ctx, p, req.Msg.GetSlotId())
	if err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.ClockInResponse{Result: resultToProto(result)}), nil
}

// ClockOut settles the caller's active slot (clock-out / early finish).
func (s *Service) ClockOut(ctx context.Context, req *connect.Request[v1.ClockOutRequest]) (*connect.Response[v1.ClockOutResponse], error) {
	p, svc, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	result, err := svc.ClockOut(ctx, p, req.Msg.GetSlotId())
	if err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.ClockOutResponse{Result: resultToProto(result)}), nil
}

// CancelSlot cancels the caller's slot.
func (s *Service) CancelSlot(ctx context.Context, req *connect.Request[v1.CancelSlotRequest]) (*connect.Response[v1.CancelSlotResponse], error) {
	p, svc, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	result, err := svc.Cancel(ctx, p, req.Msg.GetSlotId())
	if err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.CancelSlotResponse{Result: resultToProto(result)}), nil
}

// PokeOccupant nudges an overrunning occupant; it changes no schedule state.
func (s *Service) PokeOccupant(ctx context.Context, req *connect.Request[v1.PokeOccupantRequest]) (*connect.Response[v1.PokeOccupantResponse], error) {
	p, svc, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if err := svc.Poke(ctx, p, req.Msg.GetSlotId()); err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.PokeOccupantResponse{}), nil
}

// ForceClockOut boots an overrunning occupant and returns the recomputed schedule.
func (s *Service) ForceClockOut(ctx context.Context, req *connect.Request[v1.ForceClockOutRequest]) (*connect.Response[v1.ForceClockOutResponse], error) {
	p, svc, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	result, err := svc.ForceClockOut(ctx, p, req.Msg.GetSlotId())
	if err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.ForceClockOutResponse{Result: resultToProto(result)}), nil
}

// ForceNoShow reclaims a grace-lapsed slot and returns the recomputed schedule.
func (s *Service) ForceNoShow(ctx context.Context, req *connect.Request[v1.ForceNoShowRequest]) (*connect.Response[v1.ForceNoShowResponse], error) {
	p, svc, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	result, err := svc.ForceNoShow(ctx, p, req.Msg.GetSlotId())
	if err != nil {
		return nil, connectError(err)
	}
	return connect.NewResponse(&v1.ForceNoShowResponse{Result: resultToProto(result)}), nil
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
	case errors.Is(err, scheduling.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		// An unexpected error (DB failure, engine bug): surface as Internal without
		// leaking internals beyond the wrapped message.
		return connect.NewError(connect.CodeInternal, err)
	}
}
