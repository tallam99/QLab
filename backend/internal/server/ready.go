package server

import "net/http"

// readyz is the readiness probe. Every dependency is initialized — and verified —
// before the service starts serving (see cmd/server), and a dependency that fails
// to initialize stops startup entirely. So by the time this endpoint can answer,
// the service is ready: a reachable /readyz is itself the signal, and there is
// nothing to re-check per request.
func (s *Server) readyz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set(headerContentType, contentTypeJSON)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(healthBody))
}
