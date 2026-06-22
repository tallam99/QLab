// Package api implements the QLab Connect-RPC service (the qlab.v1.QlabService
// contract). Each method is one engine event (docs/ALGORITHM.md §6): it mutates
// state, runs the single reschedule, and returns the recomputed schedule.
//
// For now every method returns CodeUnimplemented — the real handlers, the engine
// wiring, and the store calls land in Phase 7. Embedding the generated
// UnimplementedQlabServiceHandler supplies that default, so Phase 7 can override
// one method at a time without the package ever failing to satisfy the interface.
package api

import (
	"net/http"

	"github.com/tallam99/qlab/backend/internal/gen/qlab/v1/qlabv1connect"
)

// Service implements qlabv1connect.QlabServiceHandler. The embedded
// UnimplementedQlabServiceHandler returns CodeUnimplemented from every method
// until Phase 7 overrides them.
type Service struct {
	qlabv1connect.UnimplementedQlabServiceHandler
}

// New builds the service. It takes no dependencies yet; Phase 7 adds the store and
// the scheduling engine here.
func New() *Service {
	return &Service{}
}

// Handler returns the mount path and the HTTP handler for the Connect service. The
// handler speaks Connect, gRPC, and gRPC-Web with both JSON and binary protobuf on
// the one path.
func (s *Service) Handler() (string, http.Handler) {
	return qlabv1connect.NewQlabServiceHandler(s)
}
