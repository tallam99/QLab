package server

import "net/http"

// HTTP header/content constants and the health response body, kept as consts so
// they have a single source of truth.
const (
	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"

	healthBody = `{"status":"ok"}` + "\n"
)

// healthz is a liveness probe: 200 means the process is up and able to respond.
// It deliberately checks nothing beyond the server itself — datastore health is
// the readiness probe's job (see readyz).
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set(headerContentType, contentTypeJSON)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(healthBody))
}
