package devapi

import (
	devv1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/dev/v1"
	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
	"github.com/tallam99/qlab/backend/internal/store"
)

// store -> proto conversions for the operator surface. The operator service speaks
// store domain types; proto lives only here (the api/convert.go pattern). The slot,
// time, and uuid conversions shared with the public API live in internal/protoconv.

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
