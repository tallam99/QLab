package server

import "net/http"

// readyBodyUnavailable is the response body while the service is still starting up.
const readyBodyUnavailable = `{"status":"unavailable"}` + "\n"

// readyz is the readiness probe: 503 until the service has initialized its
// dependencies (MarkReady), 200 afterward. Liveness (/healthz) is up from the
// start; readiness gates traffic only until startup completes. It's a one-way
// transition established at startup, not a per-request re-check.
func (s *Server) readyz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set(headerContentType, contentTypeJSON)
	if !s.ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(readyBodyUnavailable))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(healthBody))
}
