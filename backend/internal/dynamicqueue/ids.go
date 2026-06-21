package dynamicqueue

// Opaque identifiers — distinct named types so the compiler stops cross-type
// mix-ups. The engine treats every id as opaque; it never orders by id (ordering
// is SlotPriority alone — §1.3, §4).
type (
	// LabID is the tenant scope. Every slot in one engine call shares it.
	LabID string
	// ResourcePoolID is the interchangeable resource pool the queue feeds (§1.4).
	ResourcePoolID string
	// ResourceID is a specific resource within a pool. The zero value "" means
	// "unassigned" — a slot's resource is provisional until it clocks in (§1.1).
	ResourceID string
	// UserID is who booked a slot; opaque to the engine (§1.1).
	UserID string
	// SlotID is stable slot identity. It is identity only — never an ordering or
	// tie-break key (§1.3, §4).
	SlotID string
)

// IsAssigned reports whether the resource id refers to an actual resource
// (non-zero). A SCHEDULED slot may be unassigned between reschedules; an ACTIVE
// slot is always assigned (§1.1, §1.2).
func (r ResourceID) IsAssigned() bool { return r != "" }
