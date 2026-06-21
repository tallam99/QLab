package basic

import (
	"slices"
	"sort"
	"time"

	"github.com/tallam99/qlab/backend/internal/dynamicqueue"
)

// interval is a half-open free span [from, to) on one resource. The final
// interval on a resource has to == maxInstant, standing for open-ended.
type interval struct {
	from time.Time
	to   time.Time
}

// freeMap holds each resource's ordered, non-overlapping free intervals.
type freeMap map[dynamicqueue.ResourceID][]interval

// buildFree seeds every resource's availability from now, carving out the
// occupancy of pinned ACTIVE slots ([now, projectedEnd)) so scheduled slots are
// placed only into genuinely free time (ALGORITHM §5 step 1). Every resource ends
// with an open-ended tail, so a placement always exists.
func buildFree(resources []dynamicqueue.Resource, slots []dynamicqueue.Slot, now time.Time) freeMap {
	busy := make(map[dynamicqueue.ResourceID][]interval)
	for _, s := range slots {
		if !s.Status.IsPinned() {
			continue
		}
		if !s.ProjectedEnd.After(now) {
			continue // already finished by now; frees no future time
		}
		busy[s.AssignedResource] = append(busy[s.AssignedResource], interval{from: now, to: s.ProjectedEnd})
	}

	free := make(freeMap, len(resources))
	for _, r := range resources {
		free[r.ID] = freeFrom(now, mergeIntervals(busy[r.ID]))
	}
	return free
}

// freeFrom returns the complement of busy within [now, maxInstant): the gaps
// before, between, and after the (merged, sorted) busy intervals, always ending
// in the open-ended tail.
func freeFrom(now time.Time, busy []interval) []interval {
	var out []interval
	cursor := now
	for _, b := range busy {
		if b.from.After(cursor) {
			out = append(out, interval{from: cursor, to: b.from})
		}
		if b.to.After(cursor) {
			cursor = b.to
		}
	}
	return append(out, interval{from: cursor, to: maxInstant})
}

// mergeIntervals sorts by start and merges overlapping or touching intervals.
func mergeIntervals(ivs []interval) []interval {
	if len(ivs) == 0 {
		return nil
	}
	sort.Slice(ivs, func(i, j int) bool { return ivs[i].from.Before(ivs[j].from) })
	merged := []interval{ivs[0]}
	for _, iv := range ivs[1:] {
		last := &merged[len(merged)-1]
		if !iv.from.After(last.to) { // overlaps or touches the previous
			if iv.to.After(last.to) {
				last.to = iv.to
			}
			continue
		}
		merged = append(merged, iv)
	}
	return merged
}

// place finds the earliest feasible start for a slot of length d, no earlier than
// earliest, across all resources' free intervals — the argmin of t = max(from,
// earliest) at which the slot fits before the interval's end. Ties break to the
// smaller start, then the smaller resource id (determinism, §4). It returns the
// chosen start and resource, and whether the slot backfilled a bounded gap (vs.
// the resource's open-ended tail).
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
			t := laterOf(iv.from, earliest)
			if t.Add(dur).After(iv.to) {
				continue // doesn't fit this interval; try the next
			}
			// First fitting interval on this resource gives its earliest feasible
			// start (intervals are start-ordered, so later ones can only be later).
			if !found || t.Before(bestStart) || (t.Equal(bestStart) && rid < bestResource) {
				bestStart, bestResource, bestGapFill, found = t, rid, !iv.to.Equal(maxInstant), true
			}
			break
		}
	}
	return bestStart, bestResource, bestGapFill
}

// carve removes [start, start+d) from a resource's free intervals, splitting the
// one interval that contained the placement into the 0–2 fragments left on either
// side. place chose a start within exactly one free interval, so this never spans
// intervals.
func carve(free freeMap, r dynamicqueue.ResourceID, start time.Time, d dynamicqueue.Minutes) {
	end := start.Add(d.Duration())
	ivs := free[r]
	out := make([]interval, 0, len(ivs)+1)
	for _, iv := range ivs {
		if start.Before(iv.from) || end.After(iv.to) { // not the containing interval
			out = append(out, iv)
			continue
		}
		if start.After(iv.from) {
			out = append(out, interval{from: iv.from, to: start})
		}
		if iv.to.After(end) {
			out = append(out, interval{from: end, to: iv.to})
		}
	}
	free[r] = out
}

// sortedKeys returns a resource id slice in ascending order, for deterministic
// iteration over the freeMap.
func sortedKeys(free freeMap) []dynamicqueue.ResourceID {
	keys := make([]dynamicqueue.ResourceID, 0, len(free))
	for k := range free {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
