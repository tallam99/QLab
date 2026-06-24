//go:build testunit

package httpmw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCORS checks the CORS middleware: an allowed origin gets the
// Access-Control-Allow-Origin header (and preflights are answered with the
// permitted methods), a disallowed origin gets nothing, and — critically — an
// empty allow-list fails closed (no cross-origin access) instead of the
// underlying library's "allow every origin" default.
func TestCORS(t *testing.T) {
	const allowed = "https://qlab.web.app"

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name             string
		origins          []string
		method           string
		origin           string
		preflightMethod  string // non-empty marks an OPTIONS preflight (Access-Control-Request-Method)
		preflightHeaders string // Access-Control-Request-Headers on the preflight
		wantAllowOrigin  string
		wantAllowHeaders bool // expect a non-empty Access-Control-Allow-Headers
	}{
		{
			name:            "allowed origin, simple request",
			origins:         []string{allowed},
			method:          http.MethodGet,
			origin:          allowed,
			wantAllowOrigin: allowed,
		},
		{
			name:            "disallowed origin gets no CORS headers",
			origins:         []string{allowed},
			method:          http.MethodGet,
			origin:          "https://evil.example",
			wantAllowOrigin: "",
		},
		{
			name:            "preflight from allowed origin is answered",
			origins:         []string{allowed},
			method:          http.MethodOptions,
			origin:          allowed,
			preflightMethod: http.MethodPost,
			wantAllowOrigin: allowed,
		},
		{
			// The browser Connect client preflights its protocol + our auth headers;
			// all must be permitted or the call is blocked even from an allowed origin.
			name:             "preflight permits Connect and auth request headers",
			origins:          []string{allowed},
			method:           http.MethodOptions,
			origin:           allowed,
			preflightMethod:  http.MethodPost,
			preflightHeaders: "authorization,content-type,x-qlab-lab,connect-protocol-version,connect-timeout-ms",
			wantAllowOrigin:  allowed,
			wantAllowHeaders: true,
		},
		{
			name:            "empty allow-list fails closed",
			origins:         nil,
			method:          http.MethodGet,
			origin:          allowed,
			wantAllowOrigin: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := CORS(tt.origins)(ok)
			req := httptest.NewRequest(tt.method, "/", nil)
			req.Header.Set("Origin", tt.origin)
			if tt.preflightMethod != "" {
				req.Header.Set("Access-Control-Request-Method", tt.preflightMethod)
			}
			if tt.preflightHeaders != "" {
				req.Header.Set("Access-Control-Request-Headers", tt.preflightHeaders)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantAllowOrigin, rec.Header().Get("Access-Control-Allow-Origin"))
			if tt.preflightMethod != "" {
				// A preflight response must advertise the permitted methods.
				assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Methods"))
			}
			if tt.wantAllowHeaders {
				// All requested Connect/auth headers must come back as permitted.
				assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Headers"))
			}
		})
	}
}
