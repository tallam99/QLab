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
// There is no datastore yet (Postgres/Neon land in later phases), so this
// deliberately checks nothing beyond the server itself.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set(headerContentType, contentTypeJSON)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(healthBody))
}
