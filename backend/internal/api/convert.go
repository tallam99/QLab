package api

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
	"github.com/tallam99/qlab/backend/internal/scheduling"
	"github.com/tallam99/qlab/backend/internal/store"
)

// This file holds the only proto <-> domain conversions in the service; the engine
// and store never see proto (ALGORITHM §10). Times use the zero instant for "unset"
// on the domain side and a nil timestamp on the wire.

// slotToProto converts a persisted slot to its wire form.
func slotToProto(s store.Slot) *v1.Slot {
	return &v1.Slot{
		Id:                 s.ID,
		LabId:              s.LabID,
		UserId:             s.UserID,
		ResourcePoolId:     s.ResourcePoolID,
		AssignedResourceId: s.ResourceID,
		SlotPriority:       int32(s.Priority),
		Status:             slotStatusToProto(s.Status),
		DesiredStart:       timeToProto(s.DesiredStart),
		LookaheadMinutes:   s.LookaheadMinutes,
		DurationMinutes:    s.DurationMinutes,
		CommittedStart:     timeToProto(s.CommittedStart),
		ActualStart:        timeToProto(s.ActualStart),
		Note:               s.Note,
	}
}

// resultToProto converts a reschedule result (live slots + engine verdicts).
func resultToProto(r scheduling.Result) *v1.RescheduleResult {
	out := &v1.RescheduleResult{
		ResourcePoolId: r.ResourcePoolID,
		Slots:          make([]*v1.Slot, 0, len(r.Slots)),
		Positions:      make([]*v1.SlotPosition, 0, len(r.Positions)),
	}
	for _, s := range r.Slots {
		out.Slots = append(out.Slots, slotToProto(s))
	}
	for _, p := range r.Positions {
		out.Positions = append(out.Positions, &v1.SlotPosition{
			SlotId:             p.SlotID,
			ActualStart:        timeToProto(p.ActualStart),
			AssignedResourceId: p.AssignedResourceID,
			Recommitted:        p.Recommitted,
			Reclaimable:        p.Reclaimable,
		})
	}
	return out
}

// slotStatusToProto maps the persistence status enum to the wire enum (same
// labels). An unknown value maps to UNSPECIFIED rather than panicking.
func slotStatusToProto(s store.SlotStatus) v1.SlotStatus {
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

// timeToProto renders an instant, mapping the zero value (the domain's "unset") to
// a nil timestamp.
func timeToProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// timeFromProto reads a wire timestamp, mapping nil to the zero instant.
func timeFromProto(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}
