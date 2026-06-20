// Package server wires the HTTP router and middleware stack.
//
// Handlers stay thin; cross-cutting concerns (request id, panic recovery,
// structured request logging) live in middleware. Connect-RPC handlers mount
// here in a later phase — chi gives a clean place to hang shared middleware.
package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/tallam99/qlab/backend/internal/httplog"
)

// New builds the HTTP handler with the middleware stack and routes wired in.
func New(logger *slog.Logger) http.Handler {
	r := chi.NewRouter()

	// Order matters: RequestID first so every downstream layer (including our
	// logger) sees the id; Recoverer turns handler panics into 500s instead of
	// crashing the server.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(httplog.Middleware(logger))

	r.Get("/healthz", healthz)

	return r
}
