package devapi

import (
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	devv1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/dev/v1"
	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
	"github.com/tallam99/qlab/backend/internal/store"
)

// store -> proto conversions for the operator surface. The operator service speaks
// store domain types; proto lives only here (the api/convert.go pattern).

func labToProto(l store.Lab) *v1.Lab {
	return &v1.Lab{Id: l.ID.String(), Name: l.Name}
}

func userToProto(u store.User) *v1.User {
	return &v1.User{Id: u.ID.String(), Email: u.Email, FirstName: u.FirstName, LastName: u.LastName}
}

func poolToProto(p store.ResourcePool) *v1.ResourcePool {
	return &v1.ResourcePool{Id: p.ID.String(), LabId: p.LabID.String(), Kind: kindToProto(p.Kind), Name: p.Name}
}

func resourceToProto(r store.Resource) *v1.Resource {
	return &v1.Resource{
		Id: r.ID.String(), ResourcePoolId: r.ResourcePoolID.String(), LabId: r.LabID.String(),
		Kind: kindToProto(r.Kind), Name: r.Name,
	}
}

func memberToProto(m store.LabMember) *devv1.LabMember {
	return &devv1.LabMember{User: userToProto(m.User), Role: roleToProto(m.Role)}
}

func slotToProto(s store.Slot) *v1.Slot {
	return &v1.Slot{
		Id:                 s.ID.String(),
		LabId:              s.LabID.String(),
		UserId:             s.UserID.String(),
		ResourcePoolId:     s.ResourcePoolID.String(),
		AssignedResourceId: uuidStr(s.ResourceID),
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

func roleToProto(r store.LabRole) v1.LabRole {
	switch r {
	case store.LabRoleHead:
		return v1.LabRole_LAB_ROLE_HEAD
	case store.LabRoleMember:
		return v1.LabRole_LAB_ROLE_MEMBER
	default:
		return v1.LabRole_LAB_ROLE_UNSPECIFIED
	}
}

func kindToProto(k store.ResourceKind) v1.ResourceKind {
	switch k {
	case store.ResourceKindVentHood:
		return v1.ResourceKind_RESOURCE_KIND_VENT_HOOD
	default:
		return v1.ResourceKind_RESOURCE_KIND_UNSPECIFIED
	}
}

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

func uuidStr(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func timeToProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
