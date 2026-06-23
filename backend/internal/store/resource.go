package store

import "github.com/google/uuid"

//go:generate go tool enumer -type=ResourceKind -trimprefix=ResourceKind -transform=snake-upper -output=resourcekind_enumer.go

// ResourceKind identifies what a resource is. Labels match the resource_kind DB
// enum (snake-upper); the MVP ships exactly one (VENT_HOOD). The zero value,
// ResourceKindUnknown, is never valid.
type ResourceKind int

const (
	ResourceKindUnknown  ResourceKind = iota // zero value; never valid
	ResourceKindVentHood                     // the MVP's single equipment kind
)

// ResourcePool is a resource_pools row: a set of interchangeable resources a
// single queue fans out across.
type ResourcePool struct {
	ID    uuid.UUID
	LabID uuid.UUID
	Kind  ResourceKind
	Name  string
}

// Resource is a resources row: one interchangeable machine within a pool.
type Resource struct {
	ID             uuid.UUID
	ResourcePoolID uuid.UUID
	LabID          uuid.UUID
	Kind           ResourceKind
	Name           string
}
