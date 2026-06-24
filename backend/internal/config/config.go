// Package config loads service configuration from the environment.
//
// 12-factor: all config comes from env vars (Cloud Run injects PORT; we own
// QLAB_ENV). Keep this the single place env is read so wiring stays testable.
package config

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

// Config is the fully-resolved service configuration.
type Config struct {
	// Port is the TCP port the HTTP server listens on. Cloud Run injects PORT
	// and the container must respect it. The local default (8090) is deliberately
	// uncommon to dodge other tooling on this stack — Firebase emulators
	// (Firestore 8080, Auth 9099), cloud-sql-proxy (9472), Postgres (5432),
	// Vite (5173). (envconfig defaults must be tag literals, hence not a const.)
	Port string `envconfig:"PORT" default:"8090"`
	// Env names the deployment environment. Parsed into the Environment enum,
	// which rejects unknown values at load time (see Environment.Decode).
	Env Environment `envconfig:"QLAB_ENV" default:"local"`
	// DatabaseURL is the Postgres connection string (pgx format). Locally it
	// points at the Compose Postgres; on Cloud Run it's the Neon string. Required:
	// the service pings the database on boot, so there is no DB-less mode.
	DatabaseURL string `envconfig:"DATABASE_URL" required:"true"`
	// AllowedOrigins is the CORS allow-list — the Firebase Hosting origin(s) the
	// PWA is served from (the PWA and API are separate origins, decision 0001).
	// Comma-separated; set per environment to the Hosting URL(s). The local
	// default is the Vite dev server. Empty means same-origin only (fail closed).
	AllowedOrigins []string `envconfig:"CORS_ALLOWED_ORIGINS" default:"http://localhost:5173"`
	// ClockInGraceMinutes is how long after a slot's committed start a user may
	// still clock in before the slot becomes reclaimable by the next-in-line user
	// (ALGORITHM §2.3). Injected into the scheduling engine as configuration — the
	// engine never hardcodes it. Whole minutes, >= 0.
	ClockInGraceMinutes int32 `envconfig:"CLOCK_IN_GRACE_MINUTES" default:"15"`
	// FirebaseProjectID names the Firebase project whose ID tokens this service
	// verifies (the token `aud`/`iss` must match it). Each environment verifies
	// against its own project (decision 0007). Required: auth is mandatory on every
	// data RPC. Locally it is a `demo-*` id so the Auth emulator runs fully offline.
	FirebaseProjectID string `envconfig:"FIREBASE_PROJECT_ID" required:"true"`
	// FirebaseAuthEmulatorHost, when set (host:port), points the Firebase Admin SDK
	// at a local Auth emulator instead of Google's servers — local/CI only, never
	// set in staging/prod. The SDK reads the same env var itself; config carries it
	// so the dev-login token exchange targets the same host. Empty = real Firebase.
	FirebaseAuthEmulatorHost string `envconfig:"FIREBASE_AUTH_EMULATOR_HOST" default:""`
	// FirebaseWebAPIKey is the Identity Toolkit web API key the operator MintToken
	// path uses to exchange a custom token for an ID token. Required only where the
	// operator surface runs (local/staging); against the emulator any non-empty value
	// works (a fallback is used if empty).
	FirebaseWebAPIKey string `envconfig:"FIREBASE_WEB_API_KEY" default:""`
	// OperatorSecret gates the staging/local-only operator surface (qlab.dev.v1:
	// provision workspaces, mint tokens, list/teardown). When set (outside
	// production) the operator service is mounted and every call must present this
	// secret. MUST be absent in production — the service refuses to boot otherwise
	// (decision 0008). Empty = operator surface disabled.
	OperatorSecret string `envconfig:"OPERATOR_SECRET" default:""`
	// OperatorDatabaseURL is the elevated (BYPASS row-level-security) Postgres
	// connection the operator service uses for its inherently cross-tenant admin work
	// (creating labs, listing all workspaces). Required when the operator surface is
	// enabled; like OperatorSecret, must be absent in production.
	OperatorDatabaseURL string `envconfig:"OPERATOR_DATABASE_URL" default:""`
}

// Load reads and validates configuration from the environment.
func Load() (Config, error) {
	var c Config
	if err := envconfig.Process("", &c); err != nil {
		return Config{}, err
	}
	if err := c.validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// validate enforces cross-field invariants envconfig's per-field rules can't.
func (c Config) validate() error {
	// The Auth emulator is a development stand-in that skips signature verification;
	// pointing production at it would accept forged tokens. Refuse to boot. (This is
	// the config-side half of the dev-auth prod guard; the dev-login route is the
	// other half, in the server.)
	if c.Env == EnvProduction && c.FirebaseAuthEmulatorHost != "" {
		return fmt.Errorf("FIREBASE_AUTH_EMULATOR_HOST must not be set when QLAB_ENV=production")
	}
	// The operator surface (provision/impersonate at will) must never exist in
	// production — refuse to boot if any operator config is present there
	// (decision 0008). This is the config-side half; the server also never mounts
	// the operator service in production.
	if c.Env == EnvProduction && (c.OperatorSecret != "" || c.OperatorDatabaseURL != "") {
		return fmt.Errorf("OPERATOR_SECRET / OPERATOR_DATABASE_URL must not be set when QLAB_ENV=production")
	}
	// Outside production, enabling the operator surface (a secret) requires the
	// elevated DB connection it runs its cross-tenant work on.
	if c.OperatorEnabled() && c.OperatorDatabaseURL == "" {
		return fmt.Errorf("OPERATOR_DATABASE_URL is required when OPERATOR_SECRET is set")
	}
	return nil
}

// OperatorEnabled reports whether the staging/local-only operator surface should be
// mounted: never in production, and only when a gating secret is configured.
func (c Config) OperatorEnabled() bool {
	return c.Env != EnvProduction && c.OperatorSecret != ""
}

// IsLocal reports whether the service is running in the local dev environment.
func (c Config) IsLocal() bool { return c.Env == EnvLocal }

// DevAuthEnabled reports whether the development authentication aids (the
// dev-login endpoint) are available. They exist everywhere EXCEPT production —
// they are the single most dangerous surface if shipped to prod (decision 0007),
// so this is derived from the environment and cannot be turned on in production.
func (c Config) DevAuthEnabled() bool { return c.Env != EnvProduction }
