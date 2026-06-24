//go:build testunit

package config

import "testing"

// cleanEnv sets a minimal valid environment for Load, overlaying overrides. Every
// var Load reads is set explicitly (to a known value or empty) so a stray var in the
// test runner's environment can't leak in and make a case flaky. Empty operator/
// emulator vars keep the baseline a plain local config.
func cleanEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	base := map[string]string{
		"PORT":                        "8090",
		"QLAB_ENV":                    "local",
		"DATABASE_URL":                "postgres://qlab:qlab@localhost:5432/qlab",
		"CORS_ALLOWED_ORIGINS":        "http://localhost:5173",
		"CLOCK_IN_GRACE_MINUTES":      "15",
		"FIREBASE_PROJECT_ID":         "demo-qlab",
		"FIREBASE_AUTH_EMULATOR_HOST": "",
		"FIREBASE_WEB_API_KEY":        "",
		"OPERATOR_SECRET":             "",
		"OPERATOR_DATABASE_URL":       "",
	}
	for k, v := range overrides {
		base[k] = v
	}
	for k, v := range base {
		t.Setenv(k, v)
	}
}

// TestLoad checks the happy path (env parsed into the typed Config) and the two ways
// Load fails: an unparseable field (a bogus QLAB_ENV, via Environment.Decode) and a
// cross-field validate failure (the emulator in production), so both error branches
// of Load are exercised.
func TestLoad(t *testing.T) {
	t.Run("happy path parses and applies defaults", func(t *testing.T) {
		cleanEnv(t, map[string]string{"DATABASE_URL": "postgres://app/db"})
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Env != EnvLocal || cfg.Port != "8090" || cfg.DatabaseURL != "postgres://app/db" {
			t.Errorf("unexpected config: %+v", cfg)
		}
		if cfg.ClockInGraceMinutes != 15 || len(cfg.AllowedOrigins) != 1 {
			t.Errorf("defaults not applied: %+v", cfg)
		}
	})

	t.Run("invalid environment is rejected", func(t *testing.T) {
		cleanEnv(t, map[string]string{"QLAB_ENV": "bogus"})
		if _, err := Load(); err == nil {
			t.Error("Load() with QLAB_ENV=bogus: expected error, got nil")
		}
	})

	t.Run("cross-field validate failure surfaces", func(t *testing.T) {
		cleanEnv(t, map[string]string{"QLAB_ENV": "production", "FIREBASE_AUTH_EMULATOR_HOST": "localhost:9099"})
		if _, err := Load(); err == nil {
			t.Error("Load() with emulator in production: expected error, got nil")
		}
	})
}

// TestEnvironmentDecode pins the QLAB_ENV parser: known values decode, and the empty
// string, an unknown label, and "unknown" itself are all rejected (the zero value is
// never valid).
func TestEnvironmentDecode(t *testing.T) {
	tests := []struct {
		value   string
		want    Environment
		wantErr bool
	}{
		{"local", EnvLocal, false},
		{"staging", EnvStaging, false},
		{"production", EnvProduction, false},
		{"unknown", EnvUnknown, true},
		{"", EnvUnknown, true},
		{"prod", EnvUnknown, true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			var e Environment
			err := e.Decode(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Decode(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if err == nil && e != tt.want {
				t.Errorf("Decode(%q) = %v, want %v", tt.value, e, tt.want)
			}
		})
	}
}

// TestIsLocal checks the local-environment predicate the logger/exporter wiring keys on.
func TestIsLocal(t *testing.T) {
	for _, tt := range []struct {
		env  Environment
		want bool
	}{
		{EnvLocal, true},
		{EnvStaging, false},
		{EnvProduction, false},
	} {
		if got := (Config{Env: tt.env}).IsLocal(); got != tt.want {
			t.Errorf("IsLocal(%v) = %v, want %v", tt.env, got, tt.want)
		}
	}
}

// TestOperatorEnabled checks the operator-surface gate: enabled only outside
// production and only when a secret is configured.
func TestOperatorEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"staging with secret", Config{Env: EnvStaging, OperatorSecret: "s"}, true},
		{"local with secret", Config{Env: EnvLocal, OperatorSecret: "s"}, true},
		{"staging without secret", Config{Env: EnvStaging}, false},
		{"production with secret", Config{Env: EnvProduction, OperatorSecret: "s"}, false},
	}
	for _, tt := range tests {
		if got := tt.cfg.OperatorEnabled(); got != tt.want {
			t.Errorf("%s: OperatorEnabled() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestValidate checks the cross-field guards: the Auth emulator (which skips
// signature verification) must never be configured in production (it would make the
// service accept forged tokens); the operator surface must be absent in production
// and, when enabled against real Firebase, requires the web API key its token-mint
// exchange needs (otherwise MintToken fails silently).
func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		cfg       Config
		wantError bool
	}{
		{"emulator in production rejected", Config{Env: EnvProduction, FirebaseAuthEmulatorHost: "localhost:9099"}, true},
		{"emulator locally allowed", Config{Env: EnvLocal, FirebaseAuthEmulatorHost: "localhost:9099"}, false},
		{"production without emulator allowed", Config{Env: EnvProduction}, false},
		{"operator secret in production rejected", Config{Env: EnvProduction, OperatorSecret: "s"}, true},
		{"operator db url in production rejected", Config{Env: EnvProduction, OperatorDatabaseURL: "postgres://x"}, true},
		{"operator secret without db url rejected", Config{Env: EnvStaging, OperatorSecret: "s"}, true},
		// Operator against the emulator: any web API key works, so none is required.
		{"operator with db url and emulator allowed", Config{Env: EnvLocal, OperatorSecret: "s", OperatorDatabaseURL: "postgres://x", FirebaseAuthEmulatorHost: "localhost:9099"}, false},
		// Operator against real Firebase (no emulator) needs the real web API key.
		{"operator real firebase without web api key rejected", Config{Env: EnvStaging, OperatorSecret: "s", OperatorDatabaseURL: "postgres://x"}, true},
		{"operator real firebase with web api key allowed", Config{Env: EnvStaging, OperatorSecret: "s", OperatorDatabaseURL: "postgres://x", FirebaseWebAPIKey: "AIzaKey"}, false},
	}
	for _, tt := range tests {
		err := tt.cfg.validate()
		if (err != nil) != tt.wantError {
			t.Errorf("%s: validate() error = %v, wantError %v", tt.name, err, tt.wantError)
		}
	}
}
