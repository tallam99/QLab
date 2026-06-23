package api

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"

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

			authn := s.authn.Load()
			if authn == nil {
				return nil, connect.NewError(connect.CodeUnavailable, errors.New("authentication not ready"))
			}

			rawToken := bearerToken(req.Header().Get(HeaderAuthorization))
			if rawToken == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
			}

			user, err := (*authn).Authenticate(ctx, rawToken)
			if err != nil {
				return nil, authConnectError(err)
			}

			labID, err := parseUUID(req.Header().Get(HeaderSelectedLab))
			if err != nil {
				// Authenticated but no (valid) lab selected. The lab is required up front
				// (RLS is fail-closed), so this is a client error, not an auth failure.
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("missing or invalid "+HeaderSelectedLab+" header"))
			}

			ctx = principal.NewContext(ctx, principal.Principal{UserID: user.ID, LabID: labID})
			return next(ctx, req)
		}
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
// tell "you're not invited" from "your token is bad" (Unauthenticated).
func authConnectError(err error) error {
	switch {
	case errors.Is(err, authentication.ErrNotProvisioned):
		return connect.NewError(connect.CodePermissionDenied, err)
	case errors.Is(err, authentication.ErrUnauthenticated):
		return connect.NewError(connect.CodeUnauthenticated, err)
	default:
		// Infrastructure failure (DB down during provisioning, etc.).
		return connect.NewError(connect.CodeInternal, err)
	}
}
