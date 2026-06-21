package dynamicqueue

import "time"

// Minutes is a whole-minute span. Durations and lookahead are integer minutes
// (durations are inviolable and never sub-minute — §1.1, §2) and the DB stores
// them as integers, so the domain mirrors that. Instants stay time.Time.
type Minutes int32

// Duration converts to a time.Duration for arithmetic against instants.
func (m Minutes) Duration() time.Duration { return time.Duration(m) * time.Minute }

// SlotPriority is a slot's position in line: lower runs ahead. It is a unique
// total order across a pool's slots and the SOLE processing/tie-break key — id is
// never used to order (§1.3, §4). A max-priority slot (future) is just the
// smallest value (§8.3).
type SlotPriority int32

//go:generate go tool enumer -type=SlotStatus -trimprefix=SlotStatus -transform=snake-upper -output=slotstatus_enumer.go

// SlotStatus is the live lifecycle state the engine accepts. The zero value,
// SlotStatusUnknown, is never valid. The engine sees only these two states:
// COMPLETE/CANCELLED history is filtered out before the call, and NO_SHOW is an
// output Outcome (queue.go), not an input status (§1.2, §10).
type SlotStatus int

const (
	SlotStatusUnknown   SlotStatus = iota // zero value; never valid
	SlotStatusScheduled                   // waiting; the engine places it
	SlotStatusActive                      // clocked in, running now; pinned, never moved
)

// IsOpen reports whether the engine should place this slot (it is waiting).
func (s SlotStatus) IsOpen() bool { return s == SlotStatusScheduled }

// IsPinned reports whether the slot is running and must not be moved; its
// occupancy seeds resource availability (§3 principle 1, §5 step 2).
func (s SlotStatus) IsPinned() bool { return s == SlotStatusActive }

// Slot is one booking: its identity, the booker's declared earliness, its
// lifecycle state, and (for an active slot) its pinned placement. It is the
// engine's central datum (§1.1).
//
// Which fields are INPUT vs OUTPUT depends on Status:
//
//   - SCHEDULED: SlotPriority, DesiredStart, Lookahead, Duration, and
//     CommittedStart are inputs; the placement (ActualStart, AssignedResource) is
//     recomputed every call and returned in a SlotPosition, not written back here.
//   - ACTIVE: AssignedResource, ActualStart, and ProjectedEnd are inputs and
//     pinned; the engine schedules around them and never moves them.
type Slot struct {
	ID             SlotID
	UserID         UserID
	LabID          LabID
	ResourcePoolID ResourcePoolID

	SlotPriority SlotPriority
	Status       SlotStatus

	// DesiredStart is the booked/intended start: the reference point for earliness.
	DesiredStart time.Time
	// Lookahead is how far before DesiredStart the engine may pull the slot
	// (minutes ≥ 0). Earliest start = DesiredStart − Lookahead (§2). 0 means no
	// earliness — the slot is never pulled before DesiredStart.
	Lookahead Minutes
	// Duration is how long the slot runs (minutes > 0). Inviolable — the engine
	// never shortens a session (§2).
	Duration Minutes

	// CommittedStart is the start the user was last notified of — the reference for
	// re-commit (§2.2) and no-show (§2.3). Zero means never committed. Meaningful
	// for SCHEDULED slots.
	CommittedStart time.Time

	// AssignedResource, ActualStart, and ProjectedEnd describe a pinned ACTIVE
	// slot's fixed occupancy and are inputs only for ACTIVE slots; for SCHEDULED
	// slots the placement is an output (see SlotPosition) and these are ignored.
	AssignedResource ResourceID
	ActualStart      time.Time
	// ProjectedEnd is when an ACTIVE slot is expected to free its resource:
	// ActualStart+Duration normally, later while overrunning (§6, §10). Because the
	// engine reads no clock, overrun is expressed entirely through this field.
	ProjectedEnd time.Time

	Note string // opaque to the engine (§1.1)
}

// EarliestStart is the floor the engine may place this slot at: DesiredStart −
// Lookahead. The engine additionally never places a slot before Input.Now (§5).
func (s Slot) EarliestStart() time.Time {
	return s.DesiredStart.Add(-s.Lookahead.Duration())
}
