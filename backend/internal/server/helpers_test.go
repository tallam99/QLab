//go:build testunit

package server

import (
	"context"
	"testing"

	"firebase.google.com/go/v4/auth"
	"github.com/stretchr/testify/require"

	qauth "github.com/tallam99/qlab/backend/internal/auth"
	firebaseclient "github.com/tallam99/qlab/backend/internal/clients/firebase"
	"github.com/tallam99/qlab/backend/internal/logging"
)

// testFirebaseAuth builds a Firebase Auth client that constructs fully offline:
// setting the emulator host lets the SDK skip credential discovery. These unit
// tests never verify a real token, so the emulator need not be running.
func testFirebaseAuth(t *testing.T) *auth.Client {
	t.Helper()
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "127.0.0.1:9099")
	client, err := firebaseclient.New(context.Background(), firebaseclient.Options{ProjectID: "demo-qlab-test"})
	require.NoError(t, err)
	return client
}

// testOptions is the minimal valid server Options for unit tests (a no-op logger
// and an offline Firebase client, both required by New).
func testOptions(t *testing.T) Options {
	t.Helper()
	return Options{Logger: logging.Noop(), FirebaseAuth: testFirebaseAuth(t)}
}

// fakeVerifier is an auth.TokenVerifier stub for wiring tests; it rejects every
// token, which is enough to exercise that the auth interceptor is in place.
type fakeVerifier struct{}

func (fakeVerifier) Verify(context.Context, string) (qauth.Identity, error) {
	return qauth.Identity{}, qauth.ErrInvalidToken
}
