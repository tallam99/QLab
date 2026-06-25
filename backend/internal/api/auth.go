package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/tallam99/qlab/backend/internal/httpmw"
	"github.com/tallam99/qlab/backend/internal/observability"
	"github.com/tallam99/qlab/backend/internal/principal"
	"github.com/tallam99/qlab/backend/internal/services/authentication"
)

// Request header names for authentication. Authorization carries the Firebase ID
// token; HeaderSelectedLab carries the lab the caller is acting in (a user can
// belong to several labs, so identity alone doesn't determine the lab — decision
// 0006). Exported so tests and the Yaak workspace use the canonical spelling.
const (
	HeaderAuthorization = "Authorization"
	HeaderSelectedLab   = "X-QLab-Lab"
	bearerPrefix        = "Bearer "
)

// authInterceptor authenticates every Connect call at the transport edge. It runs
// before the validate interceptor and the handler, so handlers always observe a
// populated principal (or never run). It is the Phase-8 replacement for the
// dev-header middleware: same principal seam, real verification.
//
// The authentication service is attached after the store is ready (SetAuthentication),
// so it is read through an atomic pointer; a call before it is wired gets Unavailable
// — consistent with how the scheduling service is handled.
func (s *Service) authInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// Only authenticate inbound server calls; a client-side use of this
			// interceptor (none today) must pass through untouched.
			if req.Spec().IsClient {
				return next(ctx, req)
			}

			p, err := s.resolvePrincipal(ctx, req.Header())
			if err != nil {
				return nil, authResolveConnectError(err)
			}
			ctx = principal.NewContext(ctx, p)

			// Now that the caller is known, tag the RPC span and enrich the
			// request-scoped logger so every line this request emits — and the span tree —
			// carries lab_id/user_id. One structured line per authenticated RPC records the
			// procedure (the observability per-request log line; deeper layers add spans).
			observability.Annotate(ctx, observability.LabID(p.LabID), observability.UserID(p.UserID))
			logger := httpmw.LoggerFromContext(ctx).With(
				observability.KeyLabID, p.LabID.String(),
				observability.KeyUserID, p.UserID.String(),
			)
			ctx = httpmw.WithLogger(ctx, logger)
			logger.Info("rpc", "procedure", req.Spec().Procedure)

			return next(ctx, req)
		}
	}
}

// Sentinel errors resolvePrincipal returns for transport-agnostic mapping. The
// authentication-service errors (authentication.Err*) pass through unwrapped so each
// caller's mapper can distinguish them from these.
var (
	errAuthUnavailable = errors.New("authentication not ready")
	errMissingBearer   = errors.New("missing bearer token")
	errMissingLab      = errors.New("missing or invalid " + HeaderSelectedLab + " header")
)

// resolvePrincipal verifies the bearer token and the selected-lab header on a request
// and returns the caller principal. It is the single auth path shared by the Connect
// interceptor (unary RPCs) and the SSE stream handler (a plain HTTP route), so both
// surfaces authenticate identically. Returned errors are the sentinels above or an
// authentication.Err* value; callers map them to their transport's status.
func (s *Service) resolvePrincipal(ctx context.Context, header http.Header) (principal.Principal, error) {
	authn := s.authn.Load()
	if authn == nil {
		return principal.Principal{}, errAuthUnavailable
	}
	rawToken := bearerToken(header.Get(HeaderAuthorization))
	if rawToken == "" {
		return principal.Principal{}, errMissingBearer
	}
	user, err := (*authn).Authenticate(ctx, rawToken)
	if err != nil {
		return principal.Principal{}, err
	}
	labID, err := uuid.Parse(header.Get(HeaderSelectedLab))
	if err != nil {
		// Authenticated but no (valid) lab selected. The lab is required up front (RLS
		// is fail-closed), so this is a client error, not an auth failure.
		return principal.Principal{}, errMissingLab
	}
	return principal.Principal{UserID: user.ID, LabID: labID}, nil
}

// authResolveConnectError maps a resolvePrincipal error to a Connect status code.
func authResolveConnectError(err error) error {
	switch {
	case errors.Is(err, errAuthUnavailable):
		return connect.NewError(connect.CodeUnavailable, err)
	case errors.Is(err, errMissingBearer):
		return connect.NewError(connect.CodeUnauthenticated, err)
	case errors.Is(err, errMissingLab):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return authConnectError(err)
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" value,
// tolerating any case for the scheme. Returns "" when absent or malformed.
func bearerToken(header string) string {
	if len(header) < len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return ""
	}
	return strings.TrimSpace(header[len(bearerPrefix):])
}

// authConnectError maps an authentication error to a Connect status code:
// not-provisioned (valid token, no invite) is PermissionDenied so the client can
// tell "you're not invited" from "your token is bad" (Unauthenticated); an identity
// conflict (email linked to a different account) is FailedPrecondition — a data
// state an operator must resolve, not an internal error.
func authConnectError(err error) error {
	switch {
	case errors.Is(err, authentication.ErrNotProvisioned):
		return connect.NewError(connect.CodePermissionDenied, err)
	case errors.Is(err, authentication.ErrIdentityConflict):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, authentication.ErrUnauthenticated):
		return connect.NewError(connect.CodeUnauthenticated, err)
	default:
		// Infrastructure failure (DB down during provisioning, etc.).
		return connect.NewError(connect.CodeInternal, err)
	}
}
