package scheduling

import "time"

// Minutes is a whole-minute span. The spec models flexibility and duration in
// integer minutes — durations are inviolable and never sub-minute (§1.1, §2) —
// and the DB stores them as integers, so the domain mirrors that rather than
// using time.Duration and risking sub-minute drift. Instants stay time.Time.
type Minutes int32

// Duration converts to a time.Duration for arithmetic against instants.
func (m Minutes) Duration() time.Duration { return time.Duration(m) * time.Minute }

// Priority is a slot's position in line: lower runs ahead. Derived from booking
// order (and, later, max-priority slots — §8.3). The engine processes slots in
// (Priority, SlotID) order, which is what makes "never harm anyone ahead" fall
// out for free (§1.3, §5.1).
type Priority int32

//go:generate go tool enumer -type=SlotStatus -trimprefix=SlotStatus -transform=snake-upper -output=slotstatus_enumer.go

// SlotStatus is the lifecycle state of a slot (§1.2). The zero value,
// SlotStatusUnknown, is never valid — seeing it in a flow is a programmer error.
// String()/parse methods are generated (see go:generate); the wire/DB mapping
// happens at the edges, not here.
type SlotStatus int

const (
	SlotStatusUnknown   SlotStatus = iota // zero value; never valid
	SlotStatusScheduled                   // future booking the engine may place anywhere in its band
	SlotStatusActive                      // clocked in, running now on a fixed bench; pinned, never moved
	SlotStatusComplete                    // finished; immutable history
	SlotStatusCancelled                   // withdrawn by the user; immutable history
	SlotStatusNoShow                      // clock-in grace lapsed; frees its place like a cancel, recorded distinctly (§2.3)
)

// IsOpen reports whether the engine may place this slot. Only SCHEDULED slots
// are placed; the engine recomputes their start and bench every call.
func (s SlotStatus) IsOpen() bool { return s == SlotStatusScheduled }

// IsPinned reports whether the slot is running and must never be moved or
// interrupted. Its occupancy seeds bench availability (§3 principle 1, §5 step 1).
func (s SlotStatus) IsPinned() bool { return s == SlotStatusActive }

// IsHistory reports whether the slot is settled and plays no part in future
// placement: it neither moves nor occupies future bench time.
func (s SlotStatus) IsHistory() bool {
	return s == SlotStatusComplete || s == SlotStatusCancelled || s == SlotStatusNoShow
}

// Slot is one booking: its identity, the booker's declared flexibility, its
// lifecycle state, and its current placement. It is the engine's central datum
// (§1.1).
//
// Which fields are INPUT vs OUTPUT depends on Status:
//
//   - SCHEDULED: Priority, WinStart, Window, Duration are inputs; ActualStart and
//     AssignedBench are recomputed every call (any prior values are ignored).
//   - ACTIVE: ActualStart, AssignedBench, and ProjectedEnd are inputs and pinned;
//     the engine schedules around them and never moves them.
//   - history (COMPLETE/CANCELLED/NO_SHOW): ignored by the engine entirely.
type Slot struct {
	ID     SlotID
	UserID UserID
	LabID  LabID
	PoolID PoolID

	Priority Priority   // position in line; lower is ahead (§1.3)
	Status   SlotStatus // selects which fields below are input vs output

	// WinStart is the earliest acceptable start — the floor of the band. It equals
	// the booked start initially and ratchets later (never earlier) when a slot is
	// forced past its window (§2.2). The band is [WinStart, WinStart+Window].
	WinStart time.Time
	// Window is forward start-time flexibility in minutes (≥ 0). It bounds *when* a
	// slot may start, never how long it runs. Window == 0 means rigid (§2, §2.4).
	Window Minutes
	// Duration is how long the slot runs, in minutes (> 0). Inviolable — the engine
	// never shortens a session (§2).
	Duration Minutes

	// AssignedBench is the bench the schedule placed this slot on. Provisional
	// while SCHEDULED, fixed once ACTIVE. Zero ("") means unassigned (§1.1).
	AssignedBench BenchID
	// ActualStart is where the current schedule places this slot. Output for
	// SCHEDULED, fixed input for ACTIVE (§1.1).
	ActualStart time.Time
	// ProjectedEnd is, for ACTIVE slots only, the caller-injected instant the
	// running slot is expected to free its bench: ActualStart+Duration normally,
	// later while overrunning (§6, §10). Zero for non-ACTIVE slots. Because the
	// engine reads no clock, overrun is expressed entirely through this field.
	ProjectedEnd time.Time

	Note string // opaque to the engine (§1.1)
}

// Band returns the slot's acceptable start window [WinStart, WinStart+Window].
// A placement at start is silently accommodated while start ≤ end; crossing end
// forces a ratchet and a re-commit notification (§2.1, §2.2).
func (s Slot) Band() (start, end time.Time) {
	return s.WinStart, s.WinStart.Add(s.Window.Duration())
}
