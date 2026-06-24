//go:build testunit

package devapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/auth"
	"github.com/tallam99/qlab/backend/internal/devapi"
	"github.com/tallam99/qlab/backend/internal/httpmw"
	"github.com/tallam99/qlab/backend/internal/logging"
	devv1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/dev/v1"
	"github.com/tallam99/qlab/backend/internal/protogen/qlab/dev/v1/devv1connect"
	"github.com/tallam99/qlab/backend/internal/store"
)

// stubOperator is a no-op operator.Service. The gate runs in front of it, so an
// allowed call reaches it (and gets an empty, error-free result); a denied call
// never gets here — letting the test assert purely on the gate's verdict.
type stubOperator struct{}

func (stubOperator) ProvisionLab(context.Context, store.ProvisionSpec) (store.LabWorkspace, error) {
	return store.LabWorkspace{}, nil
}
func (stubOperator) MintToken(context.Context, uuid.UUID) (string, store.User, error) {
	return "", store.User{}, nil
}
func (stubOperator) ListLabs(context.Context, string) ([]store.LabSummary, error) { return nil, nil }
func (stubOperator) GetLab(context.Context, uuid.UUID) (store.LabState, error) {
	return store.LabState{}, nil
}
func (stubOperator) TeardownLab(context.Context, uuid.UUID) error { return nil }

// fakeVerifier resolves a fixed set of tokens to identities; anything else is an
// invalid token, mirroring how the real verifier reports verification failures.
type fakeVerifier struct{ byToken map[string]auth.Identity }

func (f fakeVerifier) Verify(_ context.Context, raw string) (auth.Identity, error) {
	if id, ok := f.byToken[raw]; ok {
		return id, nil
	}
	return auth.Identity{}, auth.ErrInvalidToken
}

// The operator gate must admit a caller presenting EITHER the shared secret or an
// allowlisted, verified Google login, and reject everything else (decision 0008).
// This is the only thing standing between an internet-reachable staging surface and
// "anyone can provision/impersonate", so each rejection path is pinned explicitly.
func TestOperatorGate(t *testing.T) {
	const (
		secret    = "s3cret-value"
		goodToken = "operator-google-token"  // allowlisted, email-verified
		unverTok  = "unverified-email-token" // allowlisted email but not verified
		intruder  = "intruder-token"         // verified but not allowlisted
	)
	verifier := fakeVerifier{byToken: map[string]auth.Identity{
		goodToken: {Email: "operator@example.com", EmailVerified: true},
		unverTok:  {Email: "operator@example.com", EmailVerified: false},
		intruder:  {Email: "intruder@example.com", EmailVerified: true},
	}}

	// Configure BOTH gates so one service exercises every path. The allowlist is
	// given with different casing to prove the compare is case-insensitive.
	svc := devapi.New(devapi.Options{
		Svc:           stubOperator{},
		Secret:        secret,
		Verifier:      verifier,
		AllowedEmails: []string{"Operator@Example.com"},
	})
	path, handler := svc.Handler()
	// The real router runs the request-logger middleware before this handler, so the
	// allowlist gate's audit line can read a logger from context; replicate that here
	// with a noop logger (LoggerFromContext panics if none is present).
	logged := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(httpmw.WithLogger(r.Context(), logging.Noop())))
	})
	mux := http.NewServeMux()
	mux.Handle(path, logged)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := devv1connect.NewDevServiceClient(srv.Client(), srv.URL)

	// wantOK uses connect.Code(0) as a sentinel for "no error expected".
	const ok connect.Code = 0
	tests := []struct {
		name    string
		headers map[string]string
		want    connect.Code
	}{
		{"valid secret", map[string]string{devapi.HeaderOperatorSecret: secret}, ok},
		{"allowlisted verified login", map[string]string{"Authorization": "Bearer " + goodToken}, ok},
		{"wrong secret", map[string]string{devapi.HeaderOperatorSecret: "nope"}, connect.CodePermissionDenied},
		{"unverified email", map[string]string{"Authorization": "Bearer " + unverTok}, connect.CodePermissionDenied},
		{"non-allowlisted email", map[string]string{"Authorization": "Bearer " + intruder}, connect.CodePermissionDenied},
		{"invalid token", map[string]string{"Authorization": "Bearer garbage"}, connect.CodePermissionDenied},
		{"no credentials", nil, connect.CodePermissionDenied},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := connect.NewRequest(&devv1.ListLabsRequest{})
			for k, v := range tc.headers {
				req.Header().Set(k, v)
			}
			_, err := client.ListLabs(context.Background(), req)
			if tc.want == ok {
				require.NoError(t, err)
				return
			}
			require.Equal(t, tc.want, connect.CodeOf(err))
		})
	}
}

// New must fail loudly on a wiring mistake that would leave the surface ungated or
// half-built (decision 0008's "no operator code path without a gate").
func TestNewPanics(t *testing.T) {
	verifier := fakeVerifier{}
	tests := []struct {
		name string
		opts devapi.Options
	}{
		{"nil service", devapi.Options{Secret: "x"}},
		{"no gate configured", devapi.Options{Svc: stubOperator{}}},
		{"allowlist without verifier", devapi.Options{Svc: stubOperator{}, AllowedEmails: []string{"a@b.com"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Panics(t, func() { devapi.New(tc.opts) })
		})
	}
	// A secret alone, or an allowlist with a verifier, is a valid single gate.
	require.NotPanics(t, func() { devapi.New(devapi.Options{Svc: stubOperator{}, Secret: "x"}) })
	require.NotPanics(t, func() {
		devapi.New(devapi.Options{Svc: stubOperator{}, Verifier: verifier, AllowedEmails: []string{"a@b.com"}})
	})
}
