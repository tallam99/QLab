//go:build testunit

package config

import "testing"

// TestDevAuthEnabled checks the dev-auth gate: development aids exist in every
// environment except production (decision 0007).
func TestDevAuthEnabled(t *testing.T) {
	tests := []struct {
		env  Environment
		want bool
	}{
		{EnvLocal, true},
		{EnvStaging, true},
		{EnvProduction, false},
	}
	for _, tt := range tests {
		if got := (Config{Env: tt.env}).DevAuthEnabled(); got != tt.want {
			t.Errorf("DevAuthEnabled(env=%v) = %v, want %v", tt.env, got, tt.want)
		}
	}
}

// TestValidate checks the cross-field guard: the Auth emulator (which skips
// signature verification) must never be configured in production, since it would
// make the service accept forged tokens.
func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		cfg       Config
		wantError bool
	}{
		{"emulator in production rejected", Config{Env: EnvProduction, FirebaseAuthEmulatorHost: "localhost:9099"}, true},
		{"emulator locally allowed", Config{Env: EnvLocal, FirebaseAuthEmulatorHost: "localhost:9099"}, false},
		{"production without emulator allowed", Config{Env: EnvProduction}, false},
	}
	for _, tt := range tests {
		err := tt.cfg.validate()
		if (err != nil) != tt.wantError {
			t.Errorf("%s: validate() error = %v, wantError %v", tt.name, err, tt.wantError)
		}
	}
}
