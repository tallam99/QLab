//go:build testunit

package config

import "testing"

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
