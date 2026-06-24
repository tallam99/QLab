// Package firebaseauth implements auth.TokenVerifier over the Firebase Admin SDK.
// It verifies Firebase ID tokens (signature against Firebase's rotating public
// keys, plus audience/issuer/expiry — all handled by the SDK) and extracts the
// claims the service needs. Against the local Auth emulator the same SDK skips the
// signature check but still validates the claims, so this one implementation
// serves local, staging, and production.
package firebaseauth

import (
	"context"
	"strings"

	"firebase.google.com/go/v4/auth"

	qauth "github.com/tallam99/qlab/backend/internal/auth"
)

// Claim keys in a Firebase ID token. Firebase stores these in the Token.Claims
// map (the standard email/name OIDC claims), not as typed fields.
const (
	claimEmail         = "email"
	claimEmailVerified = "email_verified"
	claimName          = "name"
)

// Verifier verifies tokens via a Firebase auth.Client.
type Verifier struct {
	client *auth.Client
}

// Compile-time guarantee of interface satisfaction.
var _ qauth.TokenVerifier = (*Verifier)(nil)

// New returns a Verifier backed by the given Firebase Auth client.
func New(client *auth.Client) *Verifier {
	return &Verifier{client: client}
}

// Verify validates rawToken and returns its identity. Any verification failure
// (expired, malformed, wrong audience, bad signature) becomes auth.ErrInvalidToken
// — the SDK's verify errors are all token-validity errors in practice, and an
// unauthenticated caller learns nothing from the specific reason.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (qauth.Identity, error) {
	token, err := v.client.VerifyIDToken(ctx, rawToken)
	if err != nil {
		// Wrap the underlying error for server-side logs while presenting the caller
		// a single opaque sentinel.
		return qauth.Identity{}, errInvalid(err)
	}
	return qauth.Identity{
		FirebaseUID: token.UID,
		// Lowercase to match the canonical lowercase users.email (the column CHECKs
		// lower(email)); provisioning looks up by this value.
		Email:         strings.ToLower(claimString(token, claimEmail)),
		EmailVerified: claimBool(token, claimEmailVerified),
		Name:          claimString(token, claimName),
	}, nil
}

// claimString reads a string claim, tolerating its absence or a non-string value.
func claimString(token *auth.Token, key string) string {
	if v, ok := token.Claims[key].(string); ok {
		return v
	}
	return ""
}

// claimBool reads a bool claim, tolerating its absence or a non-bool value (a
// missing email_verified is treated as unverified — fail closed).
func claimBool(token *auth.Token, key string) bool {
	if v, ok := token.Claims[key].(bool); ok {
		return v
	}
	return false
}

// errInvalid wraps cause behind auth.ErrInvalidToken so errors.Is(err,
// ErrInvalidToken) holds while the cause stays in the message for logs.
func errInvalid(cause error) error {
	return &invalidTokenError{cause: cause}
}

type invalidTokenError struct{ cause error }

func (e *invalidTokenError) Error() string {
	return qauth.ErrInvalidToken.Error() + ": " + e.cause.Error()
}
func (e *invalidTokenError) Unwrap() error { return qauth.ErrInvalidToken }
