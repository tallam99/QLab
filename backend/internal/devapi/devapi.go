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
	"strings"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"connectrpc.com/validate"
	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/auth"
	"github.com/tallam99/qlab/backend/internal/httpmw"
	"github.com/tallam99/qlab/backend/internal/protoconv"
	devv1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/dev/v1"
	"github.com/tallam99/qlab/backend/internal/protogen/qlab/dev/v1/devv1connect"
	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
	"github.com/tallam99/qlab/backend/internal/services/operator"
	"github.com/tallam99/qlab/backend/internal/store"
)

// HeaderOperatorSecret carries the operator secret on every DevService call from the
// CLI/curl/tests. Together with the service not being mounted in production, it keeps
// the provision/impersonate capability out of prod (decision 0008). The browser dev
// switcher uses the allowlist path below instead — never a secret in the browser.
const HeaderOperatorSecret = "X-QLab-Operator-Secret"

// headerAuthorization carries the operator's Firebase ID token (Bearer) on the
// allowlist path. Mirrors api.HeaderAuthorization; kept local to avoid coupling the
// operator transport to the public API package.
const headerAuthorization = "Authorization"

// Service implements devv1connect.DevServiceHandler over operator.Service.
type Service struct {
	devv1connect.UnimplementedDevServiceHandler
	svc operator.Service
	// hasSecret records whether the shared-secret gate is configured; secretHash is
	// the SHA-256 of that secret (storing only the digest keeps the plaintext out of
	// resident memory; the gate compares digests in constant time).
	hasSecret  bool
	secretHash [sha256.Size]byte
	// verifier + allowedEmails back the browser allowlist gate: a verified Firebase
	// (Google) login whose email is allowlisted is accepted in lieu of the secret.
	// allowedEmails is lowercased; verifier is nil when no allowlist is configured.
	verifier      auth.TokenVerifier
	allowedEmails map[string]struct{}
	validate      connect.Interceptor
	// otel opens an RPC span (named for the procedure) on every operator call, so the
	// staging operator surface is traced like the public API.
	otel connect.Interceptor
}

// Options configures the DevService transport. At least one gate must be present:
// the shared Secret (CLI/curl/tests) or a non-empty AllowedEmails allowlist with a
// Verifier (the browser dev switcher). Both may be set — two front doors to the same
// capability (decision 0008).
type Options struct {
	// Svc is the operator domain service the handlers delegate to. Required.
	Svc operator.Service
	// Secret, when non-empty, enables the X-QLab-Operator-Secret gate.
	Secret string
	// Verifier verifies an operator's Firebase ID token for the allowlist gate.
	// Required when AllowedEmails is non-empty.
	Verifier auth.TokenVerifier
	// AllowedEmails enumerates the operator Google accounts allowed to drive the
	// surface from a browser via a verified login instead of the secret.
	AllowedEmails []string
}

// New builds the DevService transport. It panics on a missing dependency, no gate
// configured, an allowlist without a verifier, or a failure building the otel
// interceptor — a wiring bug should fail loudly (no gate would mean an open surface).
func New(opts Options) *Service {
	if opts.Svc == nil {
		panic("devapi: New requires an operator.Service")
	}
	allowed := emailSet(opts.AllowedEmails)
	if opts.Secret == "" && len(allowed) == 0 {
		panic("devapi: New requires an operator secret or a non-empty email allowlist")
	}
	if len(allowed) > 0 && opts.Verifier == nil {
		panic("devapi: New requires a TokenVerifier when an email allowlist is configured")
	}
	otelInterceptor, err := otelconnect.NewInterceptor()
	if err != nil {
		panic(fmt.Sprintf("devapi: build otelconnect interceptor: %v", err))
	}
	s := &Service{
		svc:           opts.Svc,
		verifier:      opts.Verifier,
		allowedEmails: allowed,
		validate:      validate.NewInterceptor(),
		otel:          otelInterceptor,
	}
	if opts.Secret != "" {
		s.hasSecret = true
		s.secretHash = sha256.Sum256([]byte(opts.Secret))
	}
	return s
}

// emailSet lowercases and de-blanks an allowlist into a set for O(1) membership.
func emailSet(emails []string) map[string]struct{} {
	set := make(map[string]struct{}, len(emails))
	for _, e := range emails {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			set[e] = struct{}{}
		}
	}
	return set
}

// Handler returns the mount path and HTTP handler, with the OpenTelemetry span, the
// operator gate, and protovalidate in front of every method (otel outermost so the
// span wraps the gate; gate before validate: reject before validating).
func (s *Service) Handler() (string, http.Handler) {
	return devv1connect.NewDevServiceHandler(s, connect.WithInterceptors(s.otel, s.operatorGate(), s.validate))
}

// operatorGate authorizes a call by EITHER the shared operator secret (CLI/curl/
// tests) OR an allowlisted, verified Google login (the browser dev switcher) —
// decision 0008. A call presenting neither is PermissionDenied.
func (s *Service) operatorGate() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if req.Spec().IsClient {
				return next(ctx, req)
			}
			// Secret path: constant-time compare (avoids leaking the secret via timing).
			if s.hasSecret {
				got := sha256.Sum256([]byte(req.Header().Get(HeaderOperatorSecret)))
				if subtle.ConstantTimeCompare(got[:], s.secretHash[:]) == 1 {
					return next(ctx, req)
				}
			}
			// Allowlist path: a verified Google login whose email is allowlisted.
			if len(s.allowedEmails) > 0 {
				if email, ok := s.verifyOperator(ctx, req.Header().Get(headerAuthorization)); ok {
					// Audit which operator account drove the call, to bound the blast
					// radius the same way MintToken records the impersonated user.
					httpmw.LoggerFromContext(ctx).Info("operator authenticated via allowlist", "operator_email", email)
					return next(ctx, req)
				}
			}
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("invalid or missing operator credentials"))
		}
	}
}

// verifyOperator returns the verified, allowlisted operator email for a bearer
// Authorization header, or ok=false when the token is absent, invalid, has an
// unverified email, or names an email that is not allowlisted.
func (s *Service) verifyOperator(ctx context.Context, authorization string) (string, bool) {
	if s.verifier == nil {
		return "", false
	}
	raw := bearerToken(authorization)
	if raw == "" {
		return "", false
	}
	identity, err := s.verifier.Verify(ctx, raw)
	// An unverified email must not be trusted to claim an allowlisted address.
	if err != nil || !identity.EmailVerified {
		return "", false
	}
	email := strings.ToLower(strings.TrimSpace(identity.Email))
	if _, ok := s.allowedEmails[email]; !ok {
		return "", false
	}
	return email, true
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" value,
// tolerating any case for the scheme. Returns "" when absent or malformed.
func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
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
