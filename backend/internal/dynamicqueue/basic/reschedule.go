package basic

import (
	"fmt"
	"sort"
	"time"

	"github.com/tallam99/qlab/backend/internal/dynamicqueue"
)

// maxInstant stands in for "no upper bound" on a resource's final free interval.
// It is a far-future constant rather than the time.Time zero value (which means
// "unset"), so the "does the slot fit before `to`?" test works uniformly for
// open-ended tails. Durations added to real instants never approach it.
var maxInstant = time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)

// Reschedule recomputes the resource pool's queue (ALGORITHM §5). It validates the
// input, sweeps no-shows, seeds each resource's availability from pinned ACTIVE
// occupancy, then places the remaining open slots in SlotPriority order at the
// earliest feasible start across resources. It is pure and deterministic, and the
// schedule is never infeasible (§8): every resource has an open-ended tail, so a
// placement always exists. The error is reserved for malformed input.
func (e Engine) Reschedule(in dynamicqueue.Input) (dynamicqueue.Result, error) {
	if err := in.Validate(); err != nil {
		return dynamicqueue.Result{}, err
	}

	queue := make(dynamicqueue.Queue, len(in.Slots))
	var trace dynamicqueue.Trace

	// 1. No-show sweep: free scheduled slots whose grace has lapsed, before placing
	//    anyone, so the freed capacity is available to the rest (§5 step 1).
	open := make([]dynamicqueue.Slot, 0, len(in.Slots))
	for _, s := range in.Slots {
		if !s.Status.IsOpen() {
			continue // ACTIVE seeds availability (below); history never reaches here
		}
		if e.lapsed(s, in.Now) {
			queue[s.ID] = dynamicqueue.SlotPosition{Outcome: dynamicqueue.OutcomeNoShow}
			trace = append(trace, dynamicqueue.Step{
				Kind: dynamicqueue.StepKindNoShow,
				Slot: s.ID,
				At:   in.Now,
				Detail: fmt.Sprintf("no-show: committed %s + %dm grace lapsed by %s",
					fmtTime(s.CommittedStart), e.clockInGrace, fmtTime(in.Now)),
			})
			continue
		}
		open = append(open, s)
	}

	// 2. Seed each resource's free intervals from now and pinned ACTIVE occupancy.
	free := buildFree(in.Resources, in.Slots, in.Now)
	for _, rid := range sortedKeys(free) {
		trace = append(trace, dynamicqueue.Step{
			Kind:     dynamicqueue.StepKindSeedResource,
			Resource: rid,
			At:       free[rid][0].from,
			Detail:   fmt.Sprintf("free from %s", fmtTime(free[rid][0].from)),
		})
	}

	// 3. Place open slots in priority order. Each takes the earliest feasible start
	//    left after everyone ahead, so priority is respected for free (§5.1).
	sort.Slice(open, func(i, j int) bool { return open[i].SlotPriority < open[j].SlotPriority })
	for _, s := range open {
		earliest := laterOf(s.EarliestStart(), in.Now)
		start, resource, gapFill := place(free, earliest, s.Duration)
		carve(free, resource, start, s.Duration)

		recommitted := !start.Equal(s.CommittedStart)
		queue[s.ID] = dynamicqueue.SlotPosition{
			Outcome:          dynamicqueue.OutcomePlaced,
			ActualStart:      start,
			AssignedResource: resource,
			Recommitted:      recommitted,
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
	}

	return dynamicqueue.Result{Queue: queue, Trace: trace}, nil
}

// lapsed reports whether a scheduled slot's clock-in grace has passed: it was
// committed to a start and now is past committedStart + grace (§2.3). A slot with
// no committed start has never been due, so it cannot be a no-show.
func (e Engine) lapsed(s dynamicqueue.Slot, now time.Time) bool {
	if s.CommittedStart.IsZero() {
		return false
	}
	return now.After(s.CommittedStart.Add(e.clockInGrace.Duration()))
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
