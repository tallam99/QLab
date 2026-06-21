package basic

import (
	"slices"
	"time"

	"github.com/tallam99/qlab/backend/internal/dynamicqueue"
)

// interval is a free span on one resource. A bounded interval is [start, end); the
// open-ended tail interval every resource ends with is [start, +inf) and carries
// openEnded == true (end is ignored). Modelling "no upper bound" as a flag rather
// than a sentinel instant keeps an unbounded tail unrepresentable as a real time.
type interval struct {
	start     time.Time
	end       time.Time
	openEnded bool
}

// fits reports whether a span of length d starting at t lies within the interval.
func (iv interval) fits(t time.Time, d time.Duration) bool {
	if t.Before(iv.start) {
		return false
	}
	return iv.openEnded || !t.Add(d).After(iv.end)
}

// contains reports whether the span [s, e) lies within the interval.
func (iv interval) contains(s, e time.Time) bool {
	if s.Before(iv.start) {
		return false
	}
	return iv.openEnded || !e.After(iv.end)
}

// freeMap holds each resource's ordered, non-overlapping free intervals, each
// list ending in an open-ended tail.
type freeMap map[dynamicqueue.ResourceID][]interval

// buildFree seeds every resource's availability from now, carving out the
// occupancy of pinned ACTIVE slots ([now, projectedEnd)) so scheduled slots are
// placed only into genuinely free time (ALGORITHM §5 step 1). Validate guarantees
// each active slot's projectedEnd is after now, so its occupancy is non-empty and
// an overrunning active is never mistaken for a free resource.
func buildFree(resources []dynamicqueue.Resource, slots []dynamicqueue.Slot, now time.Time) freeMap {
	busy := make(map[dynamicqueue.ResourceID][]interval)
	for _, s := range slots {
		if !s.Status.IsPinned() {
			continue
		}
		busy[s.AssignedResource] = append(busy[s.AssignedResource], interval{start: now, end: s.ProjectedEnd})
	}

	// Validate guarantees at most one ACTIVE slot per resource, so each busy list
	// holds zero or one interval — no merging needed.
	free := make(freeMap, len(resources))
	for _, r := range resources {
		free[r.ID] = freeFrom(now, busy[r.ID])
	}
	return free
}

// freeFrom returns the complement of busy within [now, +inf): the gaps before,
// between, and after the (merged, sorted) busy intervals, always ending in the
// open-ended tail.
func freeFrom(now time.Time, busy []interval) []interval {
	var out []interval
	cursor := now
	for _, b := range busy {
		if b.start.After(cursor) {
			out = append(out, interval{start: cursor, end: b.start})
		}
		if b.end.After(cursor) {
			cursor = b.end
		}
	}
	return append(out, interval{start: cursor, openEnded: true})
}

// place finds the earliest feasible start for a slot of length d, no earlier than
// earliest, across all resources' free intervals — the argmin of t = max(start,
// earliest) at which the slot fits. It returns the chosen start and resource, and
// whether the slot backfilled a bounded gap (vs. the open-ended tail). Resources
// are scanned in id order and a tie on start keeps the first (smallest-id) one, so
// the result is deterministic (§4). Every resource has an open-ended tail, so a
// placement always exists (§8).
func place(free freeMap, earliest time.Time, d dynamicqueue.Minutes) (time.Time, dynamicqueue.ResourceID, bool) {
	var (
		bestStart    time.Time
		bestResource dynamicqueue.ResourceID
		bestGapFill  bool
		found        bool
	)
	dur := d.Duration()
	for _, rid := range sortedKeys(free) {
		for _, iv := range free[rid] {
			t := laterOf(iv.start, earliest)
			if !iv.fits(t, dur) {
				continue // doesn't fit this interval; try the next
			}
			// The first fitting interval on a resource gives its earliest feasible
			// start (intervals are start-ordered, so later ones can only be later).
			if !found || t.Before(bestStart) {
				bestStart, bestResource, bestGapFill, found = t, rid, !iv.openEnded, true
			}
			break
		}
	}
	return bestStart, bestResource, bestGapFill
}

// carve removes [start, start+d) from a resource's free intervals, splitting the
// one interval that contained the placement into the 0–2 fragments left on either
// side. place chose a start within exactly one free interval, so only that
// interval is split.
func carve(free freeMap, r dynamicqueue.ResourceID, start time.Time, d dynamicqueue.Minutes) {
	end := start.Add(d.Duration())
	ivs := free[r]
	out := make([]interval, 0, len(ivs)+1)
	carved := false
	for _, iv := range ivs {
		if carved || !iv.contains(start, end) {
			out = append(out, iv)
			continue
		}
		carved = true
		if start.After(iv.start) {
			out = append(out, interval{start: iv.start, end: start})
		}
		switch {
		case iv.openEnded:
			out = append(out, interval{start: end, openEnded: true})
		case iv.end.After(end):
			out = append(out, interval{start: end, end: iv.end})
		}
	}
	free[r] = out
}

// sortedKeys returns the resource ids in ascending order, for deterministic
// iteration over the freeMap.
func sortedKeys(free freeMap) []dynamicqueue.ResourceID {
	keys := make([]dynamicqueue.ResourceID, 0, len(free))
	for k := range free {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
