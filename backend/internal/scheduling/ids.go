package scheduling

// Opaque identifiers. They are distinct named types (not bare strings) so the
// compiler stops us passing, say, a BenchID where a SlotID is expected. The
// engine treats every id as opaque: it never parses or orders by them, except
// SlotID as the deterministic final tie-breaker in processing order (§1.1, §4).
type (
	// LabID is the tenant scope. Every slot in a single Reschedule call shares it.
	LabID string
	// PoolID is the interchangeable bench pool the queue feeds (§1.4).
	PoolID string
	// BenchID is a specific bench within a pool. The zero value "" means
	// "unassigned" — a slot's bench is provisional until it clocks in (§1.1).
	BenchID string
	// UserID is who booked a slot; opaque to the engine (§1.1).
	UserID string
	// SlotID is stable slot identity, and the final tie-breaker when two slots
	// sort equally by priority (§4 invariant 6).
	SlotID string
)

// IsAssigned reports whether the bench id refers to an actual bench (non-zero).
// A SCHEDULED slot may be unassigned between reschedules; an ACTIVE slot is
// always assigned (§1.1, §1.2).
func (b BenchID) IsAssigned() bool { return b != "" }
