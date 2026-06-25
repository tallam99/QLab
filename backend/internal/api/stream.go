package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tallam99/qlab/backend/internal/httpmw"
	"github.com/tallam99/qlab/backend/internal/observability"
	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
	"github.com/tallam99/qlab/backend/internal/services/authentication"
	"github.com/tallam99/qlab/backend/internal/services/scheduling"
	"github.com/tallam99/qlab/backend/internal/store"
)

const (
	// StreamSchedulePath is the Server-Sent Events endpoint the PWA subscribes to for
	// live schedule updates. It is a plain HTTP route, not a Connect RPC: SSE is a
	// long-lived text stream, which Connect's unary model doesn't express. Auth is the
	// same bearer-token + selected-lab path as the RPCs (resolvePrincipal), so the
	// stream is exactly as protected as every other endpoint (decision 0001).
	StreamSchedulePath = "/v1/stream/schedule"
	// queryResourcePoolID is the pool a stream subscribes to, as a query parameter
	// (EventSource can't send a request body). The browser client sends auth in headers.
	queryResourcePoolID = "resource_pool_id"
	// streamHeartbeatInterval is how often a comment frame is written on an idle stream,
	// to keep it alive through idle-timeout proxies and surface a dead client promptly.
	streamHeartbeatInterval = 25 * time.Second
)

// streamMarshaler renders the QueueEvent to JSON for the SSE frame. protojson's
// defaults (lowerCamelCase fields, enums as their string names) are exactly what the
// frontend's generated fromJson(QueueEventSchema, …) parses, so the wire shape needs
// no special options.
var streamMarshaler = protojson.MarshalOptions{}

// StreamSchedule is the SSE handler for a pool's live schedule. It authenticates and
// authorizes the caller (the same membership/RLS check GetSchedule runs), sends the
// current schedule as the first frame, then pushes a fresh QueueEvent whenever the
// pool's schedule changes — driven by the realtime broker, which the Postgres
// listener feeds from transactional NOTIFYs (decision 0010). Each change is a signal
// to recompute, so the handler re-runs Schedule and emits the result; it never trusts
// a serialized payload, so a client is always current.
func (s *Service) StreamSchedule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := httpmw.LoggerFromContext(ctx)

		broker := s.broker.Load()
		schedPtr := s.sched.Load()
		if broker == nil || schedPtr == nil {
			http.Error(w, "service not ready", http.StatusServiceUnavailable)
			return
		}
		sched := *schedPtr

		// SSE needs a flushable, chunked writer; if the stack can't provide one there is
		// no point starting the stream.
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		p, err := s.resolvePrincipal(ctx, r.Header)
		if err != nil {
			http.Error(w, err.Error(), authResolveHTTPStatus(err))
			return
		}
		poolID, err := uuid.Parse(r.URL.Query().Get(queryResourcePoolID))
		if err != nil {
			http.Error(w, "invalid or missing "+queryResourcePoolID, http.StatusBadRequest)
			return
		}

		// Subscribe BEFORE the authorizing read so a change between the read and the
		// subscription isn't lost: it lands in the channel and triggers a recompute.
		updates, unsubscribe := broker.Subscribe(poolID)
		defer unsubscribe()

		// Authorize and seed the stream in one read: Schedule enforces membership and
		// pool-in-lab (RLS), and its result is the initial frame, so the client is current
		// the instant it connects (closing the race with its own GetSchedule load).
		snapshot, err := sched.Schedule(ctx, p, poolID)
		if err != nil {
			http.Error(w, err.Error(), scheduleHTTPStatus(err))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		// Defeat proxy/CDN response buffering (nginx, the Cloud Run front end) that would
		// otherwise hold frames until the stream closes.
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		// Initial frame: the current schedule, kind-less (UNSPECIFIED == "here is the
		// state", not an event). Subsequent frames carry the triggering event's type.
		if err := writeScheduleEvent(w, p.LabID, poolID, "", snapshot); err != nil {
			return
		}
		flusher.Flush()

		heartbeat := time.NewTicker(streamHeartbeatInterval)
		defer heartbeat.Stop()

		for {
			select {
			case <-ctx.Done(): // client disconnected (or server shutting down)
				return
			case <-heartbeat.C:
				// An SSE comment line: ignored by clients, keeps the connection warm.
				if _, err := io.WriteString(w, ":\n\n"); err != nil {
					return
				}
				flusher.Flush()
			case n := <-updates:
				// The notification is only a signal; recompute the current schedule for this
				// subscriber. A transient read error doesn't end the stream — the next
				// notification or heartbeat recovers it.
				result, err := sched.Schedule(ctx, p, poolID)
				if err != nil {
					logger.Warn("schedule stream recompute failed",
						"error", err, observability.KeyResourcePool, poolID.String())
					continue
				}
				if err := writeScheduleEvent(w, p.LabID, poolID, n.Kind, result); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

// writeScheduleEvent writes one SSE "data:" frame carrying a QueueEvent for the given
// schedule result. kind is the triggering change ("" for the initial snapshot / a
// listener refresh).
func writeScheduleEvent(w io.Writer, labID, poolID uuid.UUID, kind store.ScheduleChangeKind, result scheduling.Result) error {
	evt := &v1.QueueEvent{
		LabId:          labID.String(),
		ResourcePoolId: poolID.String(),
		Type:           kindToEventType(kind),
		OccurredAt:     timestamppb.Now(),
		Result:         resultToProto(result),
	}
	data, err := streamMarshaler.Marshal(evt)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

// kindToEventType maps a store schedule-change kind to the wire QueueEventType. The
// empty kind (initial snapshot / listener refresh) is UNSPECIFIED, which on the stream
// means "here is the current schedule", not an error.
func kindToEventType(kind store.ScheduleChangeKind) v1.QueueEventType {
	switch kind {
	case store.ScheduleChangeSlotCreated:
		return v1.QueueEventType_QUEUE_EVENT_TYPE_SLOT_CREATED
	case store.ScheduleChangeClockedIn:
		return v1.QueueEventType_QUEUE_EVENT_TYPE_CLOCKED_IN
	case store.ScheduleChangeClockedOut:
		return v1.QueueEventType_QUEUE_EVENT_TYPE_CLOCKED_OUT
	case store.ScheduleChangeCancelled:
		return v1.QueueEventType_QUEUE_EVENT_TYPE_CANCELLED
	case store.ScheduleChangeNoShow:
		return v1.QueueEventType_QUEUE_EVENT_TYPE_NO_SHOW
	default:
		return v1.QueueEventType_QUEUE_EVENT_TYPE_UNSPECIFIED
	}
}

// authResolveHTTPStatus maps a resolvePrincipal error to an HTTP status (the SSE
// analogue of authResolveConnectError).
func authResolveHTTPStatus(err error) int {
	switch {
	case errors.Is(err, errAuthUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, errMissingBearer), errors.Is(err, authentication.ErrUnauthenticated):
		return http.StatusUnauthorized
	case errors.Is(err, authentication.ErrNotProvisioned):
		return http.StatusForbidden
	case errors.Is(err, authentication.ErrIdentityConflict):
		return http.StatusPreconditionFailed
	case errors.Is(err, errMissingLab):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// scheduleHTTPStatus maps a scheduling domain error to an HTTP status (the SSE
// analogue of connectError, used for the stream's authorizing read).
func scheduleHTTPStatus(err error) int {
	switch {
	case errors.Is(err, scheduling.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, scheduling.ErrNotMember), errors.Is(err, scheduling.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, scheduling.ErrInvalidState):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
