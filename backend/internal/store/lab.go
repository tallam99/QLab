package store

import "github.com/google/uuid"

//go:generate go tool enumer -type=LabRole -trimprefix=LabRole -transform=upper -output=labrole_enumer.go

// LabRole is a user's role within a lab. Mirrors the lab_role DB enum
// ('HEAD','MEMBER'); String()/parse are generated (see go:generate). The zero
// value, LabRoleUnknown, is never valid.
type LabRole int

const (
	LabRoleUnknown LabRole = iota // zero value; never valid
	LabRoleHead
	LabRoleMember
)

// Lab is one tenant (workspace). Labs are created by the operator tooling in
// staging/local; in the product they exist already.
type Lab struct {
	ID   uuid.UUID
	Name string
}
