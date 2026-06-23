package basic

import (
	"fmt"
	"sort"
	"time"

	"github.com/tallam99/qlab/backend/internal/dynamicqueue"
)

// Reschedule recomputes the resource pool's queue (ALGORITHM §5). It validates the
// input, seeds each resource's availability from pinned ACTIVE occupancy, then
// places the open slots in SlotPriority order at the earliest feasible start across
// resources. It is pure and deterministic, and the schedule is never infeasible
// (§8): every resource has an open-ended tail, so a placement always exists. The
// error is reserved for malformed input.
//
// A slot whose clock-in grace has lapsed is NOT freed — the engine keeps it in
// place (so it still holds its resource) and flags its placement Reclaimable, since
// the user may simply have forgotten to clock in while using the resource. Removing
// a no-show is a deliberate human act (the next-in-line user's ForceNoShow), not an
// automatic sweep (§2.3).
func (e Engine) Reschedule(in dynamicqueue.Input) (dynamicqueue.Result, error) {
	if err := in.Validate(); err != nil {
		return dynamicqueue.Result{}, err
	}

	queue := make(dynamicqueue.Queue, len(in.Slots))
	var trace dynamicqueue.Trace

	// 1. Collect the open (SCHEDULED) slots to place. ACTIVE slots seed availability
	//    (below); history never reaches the engine.
	open := make([]dynamicqueue.Slot, 0, len(in.Slots))
	for _, s := range in.Slots {
		if s.Status.IsOpen() {
			open = append(open, s)
		}
	}

	// 2. Seed each resource's free intervals from now and pinned ACTIVE occupancy.
	free := buildFree(in.Resources, in.Slots, in.Now)
	// The resource id set is fixed for the rest of the call — carve mutates interval
	// lists, never the keys — so sort once and reuse for both seeding and placement.
	order := sortedKeys(free)
	for _, rid := range order {
		trace = append(trace, dynamicqueue.Step{
			Kind:     dynamicqueue.StepKindSeedResource,
			Resource: rid,
			At:       free[rid][0].start,
			Detail:   fmt.Sprintf("free from %s", fmtTime(free[rid][0].start)),
		})
	}

	// 3. Place open slots in priority order. Each takes the earliest feasible start
	//    left after everyone ahead, so priority is respected for free (§5.1).
	sort.Slice(open, func(i, j int) bool { return open[i].SlotPriority < open[j].SlotPriority })
	for _, s := range open {
		earliest := laterOf(s.EarliestStart(), in.Now)
		start, resource, gapFill := place(free, order, earliest, s.Duration)
		carve(free, resource, start, s.Duration)

		recommitted := !start.Equal(s.CommittedStart)
		reclaimable := lapsed(s, in.Now, in.Grace)
		queue[s.ID] = dynamicqueue.SlotPosition{
			ActualStart:      start,
			AssignedResource: resource,
			Recommitted:      recommitted,
			Reclaimable:      reclaimable,
		}

		kind := dynamicqueue.StepKindPlace
		if gapFill {
			kind = dynamicqueue.StepKindGapFill
		}
		trace = append(trace, dynamicqueue.Step{
			Kind:     kind,
			Slot:     s.ID,
			Resource: resource,
			At:       start,
			Detail: fmt.Sprintf("placed %s–%s on %s",
				fmtTime(start), fmtTime(start.Add(s.Duration.Duration())), resource),
		})
		if recommitted {
			trace = append(trace, dynamicqueue.Step{
				Kind:   dynamicqueue.StepKindRecommit,
				Slot:   s.ID,
				At:     start,
				Detail: fmt.Sprintf("start changed from %s to %s", fmtTime(s.CommittedStart), fmtTime(start)),
			})
		}
		if reclaimable {
			// Grace lapsed but the slot keeps its place; the next-in-line user may
			// reclaim it (§2.3).
			trace = append(trace, dynamicqueue.Step{
				Kind: dynamicqueue.StepKindReclaimable,
				Slot: s.ID,
				At:   in.Now,
				Detail: fmt.Sprintf("reclaimable: committed %s + %dm grace lapsed by %s",
					fmtTime(s.CommittedStart), in.Grace, fmtTime(in.Now)),
			})
		}
	}

	return dynamicqueue.Result{Queue: queue, Trace: trace}, nil
}

// lapsed reports whether a scheduled slot's clock-in grace has passed: it was
// committed to a start and now is past committedStart + grace (§2.3). A slot with
// no committed start has never been due, so it cannot be a no-show. grace arrives
// per run on Input, so this is a free function, not a method.
func lapsed(s dynamicqueue.Slot, now time.Time, grace dynamicqueue.Minutes) bool {
	if s.CommittedStart.IsZero() {
		return false
	}
	return now.After(s.CommittedStart.Add(grace.Duration()))
}

// laterOf returns the later of two instants.
func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// fmtTime renders an instant for trace details; the zero value reads as "—".
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format(time.RFC3339)
}
