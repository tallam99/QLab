package server

import "net/http"

// healthz is a liveness probe: 200 means the process is up and able to respond.
// There is no datastore yet (Postgres/Neon land in later phases), so this
// deliberately checks nothing beyond the server itself.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}
