// Package devapi is the transport adapter for the staging/local-only operator
// surface: it implements the qlab.dev.v1.DevService Connect handler over an
// operator.Service, gated by the operator secret (decision 0008). It is a SEPARATE
// service from the public qlab.v1.QlabService and is mounted only outside
// production — so the production binary contains no operator capability at all.
package devapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"connectrpc.com/validate"
	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/httpmw"
	"github.com/tallam99/qlab/backend/internal/protoconv"
	devv1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/dev/v1"
	"github.com/tallam99/qlab/backend/internal/protogen/qlab/dev/v1/devv1connect"
	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
	"github.com/tallam99/qlab/backend/internal/services/operator"
	"github.com/tallam99/qlab/backend/internal/store"
)

// HeaderOperatorSecret carries the operator secret on every DevService call. It is
// the gate that, together with the service not being mounted in production, keeps
// the provision/impersonate capability out of prod (decision 0008).
const HeaderOperatorSecret = "X-QLab-Operator-Secret"

// Service implements devv1connect.DevServiceHandler over operator.Service.
type Service struct {
	devv1connect.UnimplementedDevServiceHandler
	svc operator.Service
	// secretHash is the SHA-256 of the operator secret. Storing only the digest keeps
	// the plaintext secret out of resident memory; the interceptor compares digests.
	secretHash [sha256.Size]byte
	validate   connect.Interceptor
	// otel opens an RPC span (named for the procedure) on every operator call, so the
	// staging operator surface is traced like the public API.
	otel connect.Interceptor
}

// New builds the DevService transport. It panics on a missing dependency, an empty
// secret, or a failure building the otel interceptor — a wiring bug should fail loudly
// (and an empty secret would mean no gate).
func New(svc operator.Service, secret string) *Service {
	if svc == nil {
		panic("devapi: New requires an operator.Service")
	}
	if secret == "" {
		panic("devapi: New requires a non-empty operator secret")
	}
	otelInterceptor, err := otelconnect.NewInterceptor()
	if err != nil {
		panic(fmt.Sprintf("devapi: build otelconnect interceptor: %v", err))
	}
	return &Service{svc: svc, secretHash: sha256.Sum256([]byte(secret)), validate: validate.NewInterceptor(), otel: otelInterceptor}
}

// Handler returns the mount path and HTTP handler, with the OpenTelemetry span, the
// operator-secret gate, and protovalidate in front of every method (otel outermost so
// the span wraps the gate; secret before validate: reject before validating).
func (s *Service) Handler() (string, http.Handler) {
	return devv1connect.NewDevServiceHandler(s, connect.WithInterceptors(s.otel, s.secretInterceptor(), s.validate))
}

// secretInterceptor rejects any call without the matching operator secret. A
// constant-time compare avoids leaking the secret via timing.
func (s *Service) secretInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if req.Spec().IsClient {
				return next(ctx, req)
			}
			got := sha256.Sum256([]byte(req.Header().Get(HeaderOperatorSecret)))
			if subtle.ConstantTimeCompare(got[:], s.secretHash[:]) != 1 {
				return nil, connect.NewError(connect.CodePermissionDenied, errors.New("invalid or missing operator secret"))
			}
			return next(ctx, req)
		}
	}
}

func (s *Service) ProvisionLab(ctx context.Context, req *connect.Request[devv1.ProvisionLabRequest]) (*connect.Response[devv1.ProvisionLabResponse], error) {
	ws, err := s.svc.ProvisionLab(ctx, store.ProvisionSpec{
		Feature:       req.Msg.GetFeature(),
		MemberCount:   int(req.Msg.GetMemberCount()),
		ResourceCount: int(req.Msg.GetResourceCount()),
	})
	if err != nil {
		return nil, devError(err)
	}
	out := &devv1.ProvisionLabResponse{
		Lab:       labToProto(ws.Lab),
		Pool:      poolToProto(ws.Pool),
		Members:   membersToProto(ws.Members),
		Resources: resourcesToProto(ws.Resources),
	}
	return connect.NewResponse(out), nil
}

func (s *Service) MintToken(ctx context.Context, req *connect.Request[devv1.MintTokenRequest]) (*connect.Response[devv1.MintTokenResponse], error) {
	userID, err := uuid.Parse(req.Msg.GetUserId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	token, user, err := s.svc.MintToken(ctx, userID)
	if err != nil {
		return nil, devError(err)
	}
	// Audit every impersonation: minting a token = "act as this user". The operator
	// secret is the only gate, so record who was impersonated (the request-scoped
	// logger already carries the request id) to bound the blast radius if it leaks.
	httpmw.LoggerFromContext(ctx).Warn("operator minted impersonation token",
		"impersonated_user_id", user.ID.String(), "impersonated_email", user.Email)
	return connect.NewResponse(&devv1.MintTokenResponse{IdToken: token, User: userToProto(user)}), nil
}

func (s *Service) ListLabs(ctx context.Context, req *connect.Request[devv1.ListLabsRequest]) (*connect.Response[devv1.ListLabsResponse], error) {
	labs, err := s.svc.ListLabs(ctx, req.Msg.GetFeature())
	if err != nil {
		return nil, devError(err)
	}
	out := &devv1.ListLabsResponse{Labs: make([]*devv1.LabSummary, 0, len(labs))}
	for _, l := range labs {
		out.Labs = append(out.Labs, &devv1.LabSummary{
			Lab: labToProto(l.Lab), UserCount: int32(l.UserCount), ResourceCount: int32(l.ResourceCount),
		})
	}
	return connect.NewResponse(out), nil
}

func (s *Service) GetLab(ctx context.Context, req *connect.Request[devv1.GetLabRequest]) (*connect.Response[devv1.GetLabResponse], error) {
	labID, err := uuid.Parse(req.Msg.GetLabId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	state, err := s.svc.GetLab(ctx, labID)
	if err != nil {
		return nil, devError(err)
	}
	out := &devv1.GetLabResponse{
		Lab:       labToProto(state.Lab),
		Members:   membersToProto(state.Members),
		Resources: resourcesToProto(state.Resources),
		Pools:     make([]*v1.ResourcePool, 0, len(state.Pools)),
		Slots:     make([]*v1.Slot, 0, len(state.Slots)),
	}
	for _, p := range state.Pools {
		out.Pools = append(out.Pools, poolToProto(p))
	}
	for _, sl := range state.Slots {
		out.Slots = append(out.Slots, protoconv.Slot(sl))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) TeardownLab(ctx context.Context, req *connect.Request[devv1.TeardownLabRequest]) (*connect.Response[devv1.TeardownLabResponse], error) {
	labID, err := uuid.Parse(req.Msg.GetLabId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := s.svc.TeardownLab(ctx, labID); err != nil {
		return nil, devError(err)
	}
	return connect.NewResponse(&devv1.TeardownLabResponse{}), nil
}

func membersToProto(members []store.LabMember) []*devv1.LabMember {
	out := make([]*devv1.LabMember, 0, len(members))
	for _, m := range members {
		out = append(out, memberToProto(m))
	}
	return out
}

func resourcesToProto(resources []store.Resource) []*v1.Resource {
	out := make([]*v1.Resource, 0, len(resources))
	for _, r := range resources {
		out = append(out, resourceToProto(r))
	}
	return out
}

// devError maps an operator/store error to a Connect status code.
func devError(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
