// Package protoconv holds the store-domain -> qlab.v1 wire conversions shared by
// more than one transport. The public data API (internal/api) and the operator
// surface (internal/devapi) both emit the same v1.Slot wire shape from the same
// store domain types; keeping the conversion in one place means a change to the
// slot wire mapping (a new field, a new SlotStatus value) is made once, not copied
// across packages where the copies could silently diverge.
//
// Conversions that belong to a single transport (request parsing, reschedule
// results, lab/role/kind shapes) stay in that transport's own convert.go.
package protoconv

import (
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
	"github.com/tallam99/qlab/backend/internal/store"
)

// UUID renders a uuid for the wire, mapping uuid.Nil (the domain's "unset") to an
// empty string.
func UUID(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

// Time renders an instant, mapping the zero value (the domain's "unset") to a nil
// timestamp.
func Time(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// Slot converts a persisted slot to its wire form.
func Slot(s store.Slot) *v1.Slot {
	return &v1.Slot{
		Id:                 s.ID.String(),
		LabId:              s.LabID.String(),
		UserId:             s.UserID.String(),
		ResourcePoolId:     s.ResourcePoolID.String(),
		AssignedResourceId: UUID(s.ResourceID),
		SlotPriority:       int32(s.Priority),
		Status:             SlotStatus(s.Status),
		DesiredStart:       Time(s.DesiredStart),
		LookaheadMinutes:   s.LookaheadMinutes,
		DurationMinutes:    s.DurationMinutes,
		CommittedStart:     Time(s.CommittedStart),
		ActualStart:        Time(s.ActualStart),
		Note:               s.Note,
	}
}

// SlotStatus maps the persistence status enum to the wire enum (same labels). An
// unknown value maps to UNSPECIFIED rather than panicking.
func SlotStatus(s store.SlotStatus) v1.SlotStatus {
	switch s {
	case store.SlotStatusScheduled:
		return v1.SlotStatus_SLOT_STATUS_SCHEDULED
	case store.SlotStatusActive:
		return v1.SlotStatus_SLOT_STATUS_ACTIVE
	case store.SlotStatusComplete:
		return v1.SlotStatus_SLOT_STATUS_COMPLETE
	case store.SlotStatusCancelled:
		return v1.SlotStatus_SLOT_STATUS_CANCELLED
	case store.SlotStatusNoShow:
		return v1.SlotStatus_SLOT_STATUS_NO_SHOW
	default:
		return v1.SlotStatus_SLOT_STATUS_UNSPECIFIED
	}
}
