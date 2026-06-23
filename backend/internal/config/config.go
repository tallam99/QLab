// Package config loads service configuration from the environment.
//
// 12-factor: all config comes from env vars (Cloud Run injects PORT; we own
// QLAB_ENV). Keep this the single place env is read so wiring stays testable.
package config

import "github.com/kelseyhightower/envconfig"

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
}

// Load reads and validates configuration from the environment.
func Load() (Config, error) {
	var c Config
	if err := envconfig.Process("", &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// IsLocal reports whether the service is running in the local dev environment.
func (c Config) IsLocal() bool { return c.Env == EnvLocal }
