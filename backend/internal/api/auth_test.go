//go:build testunit

package api

import (
	"errors"
	"testing"

	"connectrpc.com/connect"

	"github.com/tallam99/qlab/backend/internal/services/authentication"
)

// TestBearerToken checks extraction of the token from an Authorization header,
// including a case-insensitive scheme and malformed inputs.
func TestBearerToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer abc.def.ghi", "abc.def.ghi"},
		{"bearer abc", "abc"},     // scheme is case-insensitive
		{"BEARER abc", "abc"},     // ditto
		{"Bearer   abc  ", "abc"}, // surrounding spaces trimmed
		{"", ""},
		{"abc.def.ghi", ""}, // no scheme
		{"Basic abc", ""},   // wrong scheme
		{"Bearer", ""},      // scheme only, no token
	}
	for _, tt := range tests {
		if got := bearerToken(tt.header); got != tt.want {
			t.Errorf("bearerToken(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

// TestAuthConnectError maps authentication errors to Connect codes: a bad token is
// Unauthenticated, a valid-but-uninvited token is PermissionDenied (so the client
// can tell the two apart), and anything else is Internal.
func TestAuthConnectError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want connect.Code
	}{
		{"unauthenticated", authentication.ErrUnauthenticated, connect.CodeUnauthenticated},
		{"not provisioned", authentication.ErrNotProvisioned, connect.CodePermissionDenied},
		{"infrastructure", errors.New("db down"), connect.CodeInternal},
	}
	for _, tt := range tests {
		got := connect.CodeOf(authConnectError(tt.err))
		if got != tt.want {
			t.Errorf("%s: code = %v, want %v", tt.name, got, tt.want)
		}
	}
}
