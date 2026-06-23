package store

//go:generate go tool enumer -type=ResourceKind -trimprefix=ResourceKind -transform=snake-upper -output=resourcekind_enumer.go

// ResourceKind identifies what a resource is. Labels match the resource_kind DB
// enum (snake-upper); the MVP ships exactly one (VENT_HOOD). The zero value,
// ResourceKindUnknown, is never valid.
type ResourceKind int

const (
	ResourceKindUnknown  ResourceKind = iota // zero value; never valid
	ResourceKindVentHood                     // the MVP's single equipment kind
)

// Pool is a resource_pools row: a set of interchangeable resources a single queue
// fans out across.
type Pool struct {
	ID    string
	LabID string
	Kind  ResourceKind
	Name  string
}

// Resource is a resources row: one interchangeable machine within a pool.
type Resource struct {
	ID             string
	ResourcePoolID string
	LabID          string
	Kind           ResourceKind
	Name           string
}
