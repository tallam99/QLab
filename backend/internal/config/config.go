// Package config loads service configuration from the environment.
//
// 12-factor: all config comes from env vars (Cloud Run injects PORT; we own
// QLAB_ENV). Keep this the single place env is read so wiring stays testable.
package config

import "github.com/kelseyhightower/envconfig"

// Config is the fully-resolved service configuration.
type Config struct {
	// Port is the TCP port the HTTP server listens on. Cloud Run injects PORT
	// and the container must respect it.
	Port string `envconfig:"PORT" default:"8080"`
	// Env names the deployment environment: "local", "staging", or "prod".
	// It drives log format now, and dev-only route guards later (see PLAN.md).
	Env string `envconfig:"QLAB_ENV" default:"local"`
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
func (c Config) IsLocal() bool { return c.Env == "local" }
