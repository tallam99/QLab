package api

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tallam99/qlab/backend/internal/protoconv"
	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
	"github.com/tallam99/qlab/backend/internal/services/scheduling"
)

// This file holds the request-side proto -> domain conversions and the
// reschedule-result conversion; the shared store -> wire conversions (slot, time,
// uuid) live in internal/protoconv. The engine and store never see proto
// (ALGORITHM §10). Times use the zero instant for "unset" on the domain side and a
// nil timestamp on the wire.

// resultToProto converts a reschedule result (live slots + engine verdicts).
func resultToProto(r scheduling.Result) *v1.RescheduleResult {
	out := &v1.RescheduleResult{
		ResourcePoolId: r.ResourcePoolID.String(),
		Slots:          make([]*v1.Slot, 0, len(r.Slots)),
		Positions:      make([]*v1.SlotPosition, 0, len(r.Positions)),
	}
	for _, s := range r.Slots {
		out.Slots = append(out.Slots, protoconv.Slot(s))
	}
	for _, p := range r.Positions {
		out.Positions = append(out.Positions, &v1.SlotPosition{
			SlotId:             p.SlotID.String(),
			ActualStart:        protoconv.Time(p.ActualStart),
			AssignedResourceId: protoconv.UUID(p.AssignedResourceID),
			Recommitted:        p.Recommitted,
			Reclaimable:        p.Reclaimable,
		})
	}
	return out
}

// timeFromProto reads a wire timestamp, mapping nil to the zero instant.
func timeFromProto(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}
