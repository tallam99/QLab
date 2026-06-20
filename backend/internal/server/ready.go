package server

import (
	"context"
	"net/http"
	"time"

	"github.com/tallam99/qlab/backend/internal/httpmw"
)

// readyTimeout bounds the database ping behind the readiness probe so a hung DB
// can't hang the probe (and the load balancer behind it).
const readyTimeout = 2 * time.Second

// readyBody is the readiness failure response (liveness is unaffected by the DB).
const readyBody = `{"status":"unavailable"}` + "\n"

// Pinger is the slice of the DB pool the readiness probe needs. Kept as a narrow
// interface so the server package doesn't depend on pgx directly and tests can
// supply a fake. *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// readyz is a readiness probe: 200 only when the process can reach the database,
// 503 otherwise. Unlike healthz (liveness), this gates whether the instance
// should receive traffic. A nil pinger reports unavailable rather than panicking.
func readyz(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()

		w.Header().Set(headerContentType, contentTypeJSON)

		if db == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(readyBody))
			return
		}
		if err := db.Ping(ctx); err != nil {
			httpmw.FromContext(r.Context()).Warn("readiness ping failed", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(readyBody))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(healthBody))
	}
}
