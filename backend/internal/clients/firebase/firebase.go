// Package firebase owns Firebase connection setup — the "client tech", not auth
// policy. It hands back a ready *auth.Client; verifying tokens and resolving them
// to users lives behind internal/auth and internal/services/authentication. This
// is the Firebase sibling to internal/clients/postgres.
package firebase

import (
	"context"
	"fmt"
	"os"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
)

// emulatorHostEnvVar is the env var the Firebase Admin SDK itself reads to target
// a local Auth emulator. We don't set it here (the process environment does), but
// we name it so New can log/relay whether the emulator path is active.
const emulatorHostEnvVar = "FIREBASE_AUTH_EMULATOR_HOST"

// Options configures New. A struct (rather than positional params) so the client
// can take new knobs without churning call sites.
type Options struct {
	// ProjectID is the Firebase project whose tokens we verify (the token aud/iss
	// must match). Required: with no project id the SDK can't validate the audience.
	ProjectID string
}

// New builds the Firebase Auth client. When FIREBASE_AUTH_EMULATOR_HOST is set in
// the environment the SDK targets the local Auth emulator and needs no credentials
// (it skips signature verification); otherwise it uses Application Default
// Credentials (the Cloud Run service account in staging/prod). Construction does
// not dial — reachability surfaces on the first VerifyIDToken.
func New(ctx context.Context, opts Options) (*auth.Client, error) {
	if opts.ProjectID == "" {
		return nil, fmt.Errorf("firebase: ProjectID is required")
	}
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: opts.ProjectID})
	if err != nil {
		return nil, fmt.Errorf("firebase: new app: %w", err)
	}
	client, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase: auth client: %w", err)
	}
	return client, nil
}

// UsingEmulator reports whether the SDK is pointed at the local Auth emulator
// (FIREBASE_AUTH_EMULATOR_HOST set). Callers log this so it's obvious in startup
// logs that real token verification is NOT in effect.
func UsingEmulator() bool { return os.Getenv(emulatorHostEnvVar) != "" }
