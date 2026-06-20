package scheduling

import "time"

// Placement is where the engine decided one slot runs: the output for a single
// SCHEDULED slot (§5). The caller applies it back to the persisted slot and,
// when Recommitted is set, notifies the user of the new start.
type Placement struct {
	SlotID SlotID

	// ActualStart is where the slot now runs; occupancy is [ActualStart,
	// ActualStart+Duration].
	ActualStart time.Time
	// AssignedBench is the bench the slot landed on.
	AssignedBench BenchID

	// WinStart is the slot's band floor after this call: equal to the input
	// WinStart when the slot stayed within its window, or raised to ActualStart
	// when the slot was forced past its band (a ratchet — §2.2).
	WinStart time.Time
	// Recommitted is true exactly when WinStart ratcheted this call — the slot
	// could not be kept within its window and was re-committed to a later start.
	// It is the single notify signal the engine emits (§5); other notification
	// triggers (pulled-forward, bench-freed) are derived by the caller from the
	// schedule diff, not here.
	Recommitted bool
}

// Schedule is the engine's output: a Placement for every SCHEDULED slot in the
// Input. ACTIVE and history slots are absent — they are untouched by definition
// (§4 invariant 5). The schedule never "fails": there is always a valid
// placement (§8).
type Schedule struct {
	Placements []Placement
}

// Placement returns the placement for a slot, if the schedule has one.
func (s Schedule) Placement(id SlotID) (Placement, bool) {
	for _, p := range s.Placements {
		if p.SlotID == id {
			return p, true
		}
	}
	return Placement{}, false
}

// BySlot indexes placements by slot id for direct lookup by the caller.
func (s Schedule) BySlot() map[SlotID]Placement {
	m := make(map[SlotID]Placement, len(s.Placements))
	for _, p := range s.Placements {
		m[p.SlotID] = p
	}
	return m
}
