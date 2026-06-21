package dynamicqueue

//go:generate go tool enumer -type=ResourceKind -trimprefix=ResourceKind -transform=snake-upper -output=resourcekind_enumer.go

// ResourceKind identifies what a resource is. The engine is kind-agnostic — it
// schedules across interchangeable resources within one pool — but the kind lets
// the rest of the system model different equipment someone might queue for. The
// zero value, ResourceKindUnknown, is never valid. The MVP ships exactly one kind
// (§1.4).
type ResourceKind int

const (
	ResourceKindUnknown  ResourceKind = iota // zero value; never valid
	ResourceKindVentHood
)

// Resource is one interchangeable machine within a pool. A pool's single-file
// queue fans out across its resources, so the no-overlap invariant is per
// resource (§1.4, §4). A Resource carries no schedule of its own: occupancy is
// derived from the slots placed on it. The engine only needs the set of resources
// it may place onto.
type Resource struct {
	ID     ResourceID
	PoolID PoolID
	Kind   ResourceKind
}
