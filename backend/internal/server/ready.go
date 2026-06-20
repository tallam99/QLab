package server

import (
	"context"
	"net/http"
	"time"

	"github.com/tallam99/qlab/backend/internal/httpmw"
)

// readyTimeout bounds the readiness check so a hung dependency can't hang the
// probe (and the load balancer behind it).
const readyTimeout = 2 * time.Second

// readyBody is the response body when the service is not ready to serve traffic.
const readyBody = `{"status":"unavailable"}` + "\n"

// readyz is a readiness probe: 200 only when the service's dependencies are
// reachable, 503 otherwise. Unlike healthz (liveness), this gates whether the
// instance should receive traffic.
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()

	w.Header().Set(headerContentType, contentTypeJSON)

	if err := s.ready(ctx); err != nil {
		httpmw.LoggerFromContext(r.Context()).Warn("readiness check failed: dependency unreachable", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(readyBody))
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(healthBody))
}
