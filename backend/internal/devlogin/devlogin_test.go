//go:build testunit

package devlogin

import "testing"

// TestExchangeEndpoint checks the signInWithCustomToken URL: the emulator host when
// set (plain http), Google's Identity Toolkit otherwise (https), and the API-key
// fallback used against the emulator when none is configured.
func TestExchangeEndpoint(t *testing.T) {
	tests := []struct {
		name         string
		emulatorHost string
		apiKey       string
		want         string
	}{
		{
			name:         "emulator with explicit key",
			emulatorHost: "localhost:9099",
			apiKey:       "my-key",
			want:         "http://localhost:9099/identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken?key=my-key",
		},
		{
			name:         "emulator with fallback key",
			emulatorHost: "localhost:9099",
			apiKey:       "",
			want:         "http://localhost:9099/identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken?key=" + emulatorFallbackAPIKey,
		},
		{
			name:         "real identity toolkit",
			emulatorHost: "",
			apiKey:       "prod-key",
			want:         "https://identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken?key=prod-key",
		},
	}
	for _, tt := range tests {
		if got := exchangeEndpoint(tt.emulatorHost, tt.apiKey); got != tt.want {
			t.Errorf("%s: exchangeEndpoint(%q, %q) = %q, want %q", tt.name, tt.emulatorHost, tt.apiKey, got, tt.want)
		}
	}
}
