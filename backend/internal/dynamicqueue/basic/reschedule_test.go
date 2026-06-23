//go:build testunit

// Table-driven coverage of the docs/ALGORITHM.md §11 matrix plus the §4 invariant
// assertions (run on every case). The invariant checks live here, in the test
// suite, not in the engine's runtime path.
package basic

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tallam99/qlab/backend/internal/dynamicqueue"
)

const pool = dynamicqueue.ResourcePoolID("pool-1")

// base anchors every test instant; at(m) is m minutes after 09:00.
var base = time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)

func at(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }

// sched builds a SCHEDULED slot. committed < 0 means "no committed start";
// otherwise it is at(committed). All minute args are offsets from 09:00.
func sched(id string, prio, desired, dur, lookahead, committed int) dynamicqueue.Slot {
	s := dynamicqueue.Slot{
		ID:             dynamicqueue.SlotID(id),
		ResourcePoolID: pool,
		SlotPriority:   dynamicqueue.SlotPriority(prio),
		Status:         dynamicqueue.SlotStatusScheduled,
		DesiredStart:   at(desired),
		Lookahead:      dynamicqueue.Minutes(lookahead),
		Duration:       dynamicqueue.Minutes(dur),
	}
	if committed >= 0 {
		s.CommittedStart = at(committed)
	}
	return s
}

// active builds an ACTIVE slot pinned to resource until projectedEnd.
func active(id string, prio int, resource string, actualStart, projectedEnd int) dynamicqueue.Slot {
	return dynamicqueue.Slot{
		ID:               dynamicqueue.SlotID(id),
		ResourcePoolID:   pool,
		SlotPriority:     dynamicqueue.SlotPriority(prio),
		Status:           dynamicqueue.SlotStatusActive,
		AssignedResource: dynamicqueue.ResourceID(resource),
		ActualStart:      at(actualStart),
		ProjectedEnd:     at(projectedEnd),
		Duration:         dynamicqueue.Minutes(projectedEnd - actualStart), // satisfies Validate; unused for ACTIVE
	}
}

func resources(ids ...string) []dynamicqueue.Resource {
	rs := make([]dynamicqueue.Resource, len(ids))
	for i, id := range ids {
		rs[i] = dynamicqueue.Resource{
			ID:             dynamicqueue.ResourceID(id),
			ResourcePoolID: pool,
			Kind:           dynamicqueue.ResourceKindVentHood,
		}
	}
	return rs
}

// want is the expected verdict for one slot: its placement plus the recommit and
// reclaimable flags. Every SCHEDULED slot is placed (the schedule never fails, §8).
type want struct {
	start       int
	resource    string
	recommitted bool
	reclaimable bool
}

func placed(start int, resource string, recommitted bool) want {
	return want{start: start, resource: resource, recommitted: recommitted}
}

// reclaimableAt is a placed slot whose clock-in grace has lapsed: it keeps its
// place (the engine never auto-frees a no-show) but is flagged for the next-in-line
// user to reclaim (§2.3).
func reclaimableAt(start int, resource string, recommitted bool) want {
	return want{start: start, resource: resource, recommitted: recommitted, reclaimable: true}
}

type tc struct {
	name      string
	grace     int
	now       int
	resources []dynamicqueue.Resource
	slots     []dynamicqueue.Slot
	want      map[string]want
}

func TestReschedule(t *testing.T) {
	cases := []tc{
		// ---- single resource ----
		{
			name:      "empty queue, single active slot — no-op",
			resources: resources("r1"),
			slots:     []dynamicqueue.Slot{active("a", 0, "r1", 0, 60)},
			want:      map[string]want{},
		},
		{
			name:      "capacity within lookahead pulls a slot earlier than desired",
			resources: resources("r1"),
			slots: []dynamicqueue.Slot{
				active("a", 0, "r1", 0, 60),   // frees r1 at 10:00
				sched("b", 1, 90, 60, 30, 90), // desired 10:30, floor 10:00, committed 10:30
			},
			want: map[string]want{"b": placed(60, "r1", true)}, // 10:00, moved earlier
		},
		{
			name:      "lookahead 0 never pulls before desired",
			resources: resources("r1"),
			slots:     []dynamicqueue.Slot{sched("b", 1, 90, 60, 0, 90)}, // desired 10:30, floor 10:30
			want:      map[string]want{"b": placed(90, "r1", false)},     // idles until 10:30
		},
		{
			name:      "overrun pushes a slot later — recommit",
			resources: resources("r1"),
			slots: []dynamicqueue.Slot{
				active("a", 0, "r1", 0, 80),  // overruns to 10:20
				sched("b", 1, 60, 60, 0, 60), // desired/committed 10:00
			},
			want: map[string]want{"b": placed(80, "r1", true)}, // pushed to 10:20
		},
		{
			name:      "unchanged start does not recommit",
			resources: resources("r1"),
			slots:     []dynamicqueue.Slot{sched("b", 1, 60, 60, 0, 60)}, // committed 10:00, placed 10:00
			want:      map[string]want{"b": placed(60, "r1", false)},
		},
		{
			name:      "early finish pulls forward, even ahead of desired",
			now:       60, // 10:00; the active slot just finished
			resources: resources("r1"),
			slots:     []dynamicqueue.Slot{sched("b", 1, 90, 60, 30, 85)}, // desired 10:30, floor 10:00, committed 10:25
			want:      map[string]want{"b": placed(60, "r1", true)},       // 10:00
		},
		{
			name:      "chain propagates down a resource",
			resources: resources("r1"),
			slots: []dynamicqueue.Slot{
				active("a", 0, "r1", 0, 80),    // overruns to 10:20
				sched("b", 1, 60, 60, 0, 60),   // 10:00 -> pushed to 10:20
				sched("c", 2, 120, 30, 0, 120), // 11:00 -> pushed to 11:20 behind b
			},
			want: map[string]want{
				"b": placed(80, "r1", true),
				"c": placed(140, "r1", true),
			},
		},
		// ---- multi-resource ----
		{
			name:      "free resource absorbs a delay",
			resources: resources("r1", "r2"),
			slots: []dynamicqueue.Slot{
				active("a", 0, "r1", 0, 80),  // r1 busy until 10:20
				sched("b", 1, 60, 60, 0, 60), // desired/committed 10:00
			},
			want: map[string]want{"b": placed(60, "r2", false)}, // hops to free r2 at 10:00
		},
		{
			name:      "gap-fill promotes a shorter lower-priority slot",
			resources: resources("r1"),
			slots: []dynamicqueue.Slot{
				active("a", 0, "r1", 0, 60),  // frees r1 at 10:00
				sched("b", 1, 90, 60, 0, 90), // 10:30, leaves a 10:00–10:30 gap
				sched("c", 2, 60, 60, 0, 60), // 60m can't fit the 30m gap
				sched("d", 3, 60, 30, 0, 60), // 30m fits the gap
			},
			want: map[string]want{
				"b": placed(90, "r1", false), // 10:30
				"c": placed(150, "r1", true), // after b, 11:30
				"d": placed(60, "r1", false), // gap-filled at 10:00
			},
		},
		{
			name:      "promotion undone when the gap shrinks",
			resources: resources("r1"),
			slots: []dynamicqueue.Slot{
				active("a", 0, "r1", 0, 75),  // overruns to 10:15; gap is now 10:15–10:30 (15m)
				sched("b", 1, 90, 60, 0, 90), // 10:30
				sched("c", 2, 60, 60, 0, 60), // after b, 11:30
				sched("d", 3, 60, 30, 0, 60), // no longer fits the 15m gap -> behind c
			},
			want: map[string]want{
				"b": placed(90, "r1", false), // 10:30
				"c": placed(150, "r1", true), // 11:30
				"d": placed(210, "r1", true), // 12:30, behind c
			},
		},
		{
			name:      "uneven durations across resources keep per-resource order",
			resources: resources("r1", "r2"),
			slots: []dynamicqueue.Slot{
				sched("a", 1, 0, 90, 0, 0),  // 90m, committed/placed 09:00
				sched("b", 2, 0, 30, 0, 0),  // 30m, committed/placed 09:00
				sched("c", 3, 0, 60, 0, 30), // 60m, committed/placed 09:30 (after b)
			},
			want: map[string]want{
				"a": placed(0, "r1", false),  // 09:00 on r1
				"b": placed(0, "r2", false),  // 09:00 on r2
				"c": placed(30, "r2", false), // 09:30 on r2, after b
			},
		},
		// ---- lifecycle ----
		{
			name:      "grace lapsed flags a slot reclaimable but keeps its place",
			grace:     15,
			now:       20, // 09:20
			resources: resources("r1"),
			slots: []dynamicqueue.Slot{
				sched("b", 1, 0, 60, 0, 0),    // committed 09:00; grace lapsed at 09:15
				sched("c", 2, 90, 60, 30, 90), // desired 10:30, floor 10:00, committed 10:30
			},
			want: map[string]want{
				// b is NOT freed: it holds r1 from 09:20 (clamped to now), flagged
				// reclaimable. c therefore waits behind it (10:20), not pulled to 10:00.
				"b": reclaimableAt(20, "r1", true),
				"c": placed(80, "r1", true),
			},
		},
		{
			name:      "scheduled slot cancelled upstream pulls the next forward",
			resources: resources("r1"),
			// the slot that was ahead of c is simply absent (cancelled/filtered);
			// c, previously committed to 10:25, now pulls forward to its floor.
			slots: []dynamicqueue.Slot{sched("c", 2, 60, 60, 0, 85)},
			want:  map[string]want{"c": placed(60, "r1", true)},
		},
		{
			name:      "freed resource (active cleared) pulls a slot forward",
			now:       60, // 10:00; the active slot was just cleared
			resources: resources("r1"),
			slots:     []dynamicqueue.Slot{sched("b", 1, 60, 60, 0, 85)}, // committed 10:25 -> 10:00
			want:      map[string]want{"b": placed(60, "r1", true)},
		},
		{
			name:      "booking inserts at the earliest feasible start within reach",
			resources: resources("r1"),
			slots: []dynamicqueue.Slot{
				sched("a", 1, 0, 60, 0, 0),   // 09:00–10:00
				sched("b", 2, 30, 30, 0, -1), // brand-new booking, desired 09:30
			},
			want: map[string]want{
				"a": placed(0, "r1", false), // 09:00
				"b": placed(60, "r1", true), // pushed to 10:00 behind a; first commit notifies
			},
		},
		{
			name:      "long slot far in the future lands on the open-ended tail",
			resources: resources("r1"),
			slots:     []dynamicqueue.Slot{sched("a", 1, 600, 480, 0, 600)}, // desired 19:00, runs 8h
			want:      map[string]want{"a": placed(600, "r1", false)},
		},
		// ---- interactions (multiple mechanisms at once) ----
		{
			name:      "a reclaimable slot holds its resource; a free resource absorbs the next",
			grace:     15,
			now:       20, // 09:20
			resources: resources("r1", "r2"),
			slots: []dynamicqueue.Slot{
				sched("b", 1, 0, 60, 0, 0),    // committed 09:00; grace lapsed -> reclaimable, holds r1
				sched("c", 2, 30, 60, 30, -1), // brand-new booking, floor 09:00, clamped to now
			},
			want: map[string]want{
				// b keeps r1 (not freed); c hops to the free r2 rather than waiting —
				// reclaim doesn't stall multi-resource flow.
				"b": reclaimableAt(20, "r1", true),
				"c": placed(20, "r2", true),
			},
		},
		{
			name:      "overrun pushes a slot that hops to a free resource",
			resources: resources("r1", "r2"),
			slots: []dynamicqueue.Slot{
				active("a", 0, "r1", 0, 80),  // overruns to 10:20
				sched("b", 1, 60, 60, 0, 60), // hops to free r2 at 10:00
				sched("c", 2, 60, 30, 0, 60), // takes r1 after the overrun, 10:20
			},
			want: map[string]want{
				"b": placed(60, "r2", false),
				"c": placed(80, "r1", true),
			},
		},
		{
			name:      "lookahead lets a slot reach an earlier gap",
			resources: resources("r1"),
			slots: []dynamicqueue.Slot{
				active("a", 0, "r1", 0, 60),     // frees r1 at 10:00
				sched("b", 1, 90, 60, 0, 90),    // 10:30, leaves a 10:00–10:30 gap
				sched("c", 2, 120, 30, 60, 120), // desired 11:00, lookahead 60 -> floor 10:00 reaches the gap
			},
			want: map[string]want{
				"b": placed(90, "r1", false),
				"c": placed(60, "r1", true), // gap-filled at 10:00
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := dynamicqueue.Input{
				ResourcePoolID: pool,
				Slots:          c.slots,
				Resources:      c.resources,
				Now:            at(c.now),
			}
			eng := New(Config{ClockInGrace: dynamicqueue.Minutes(c.grace)})

			res, err := eng.Reschedule(in)
			require.NoError(t, err)
			checkQueue(t, c.want, res.Queue)
			assertInvariants(t, in, res, dynamicqueue.Minutes(c.grace))

			// Determinism (§4 invariant 6): identical input -> identical output.
			res2, err := eng.Reschedule(in)
			require.NoError(t, err)
			assert.Equal(t, res.Queue, res2.Queue, "queue not deterministic")
			assert.Equal(t, res.Trace, res2.Trace, "trace not deterministic")
		})
	}
}

func checkQueue(t *testing.T, want map[string]want, got dynamicqueue.Queue) {
	t.Helper()
	require.Len(t, got, len(want), "queue size")
	for id, w := range want {
		pos, ok := got[dynamicqueue.SlotID(id)]
		require.Truef(t, ok, "slot %q missing from queue", id)
		assert.Truef(t, at(w.start).Equal(pos.ActualStart), "slot %q start: want %s got %s", id, at(w.start), pos.ActualStart)
		assert.Equalf(t, dynamicqueue.ResourceID(w.resource), pos.AssignedResource, "slot %q resource", id)
		assert.Equalf(t, w.recommitted, pos.Recommitted, "slot %q recommit flag", id)
		assert.Equalf(t, w.reclaimable, pos.Reclaimable, "slot %q reclaimable flag", id)
	}
}

// assertInvariants checks the §4 post-conditions of a result against its input.
func assertInvariants(t *testing.T, in dynamicqueue.Input, res dynamicqueue.Result, grace dynamicqueue.Minutes) {
	t.Helper()
	byID := make(map[dynamicqueue.SlotID]dynamicqueue.Slot, len(in.Slots))
	for _, s := range in.Slots {
		byID[s.ID] = s
	}

	type span struct {
		start, end time.Time
		id         dynamicqueue.SlotID
	}
	occupied := make(map[dynamicqueue.ResourceID][]span)

	for id, pos := range res.Queue {
		s, ok := byID[id]
		require.Truef(t, ok, "queue references unknown slot %q", id)
		assert.Truef(t, s.Status.IsOpen(), "queue includes non-scheduled slot %q", id)

		// Every queued slot is placed — the schedule never fails (§8).
		// Earliness floor + no time travel (invariants 4, 7).
		assert.Falsef(t, pos.ActualStart.Before(s.EarliestStart()), "slot %q starts before its earliness floor", id)
		assert.Falsef(t, pos.ActualStart.Before(in.Now), "slot %q starts before now", id)
		assert.Truef(t, pos.AssignedResource.IsAssigned(), "placed slot %q has no resource", id)
		// recommit flag is exactly "start changed from committed" (§2.2).
		assert.Equalf(t, !pos.ActualStart.Equal(s.CommittedStart), pos.Recommitted, "slot %q recommit flag", id)
		// reclaimable flag is exactly "committed start + grace has lapsed" (§2.3): a
		// no-show keeps its place, flagged, rather than being freed.
		wantReclaimable := !s.CommittedStart.IsZero() && in.Now.After(s.CommittedStart.Add(grace.Duration()))
		assert.Equalf(t, wantReclaimable, pos.Reclaimable, "slot %q reclaimable flag", id)
		end := pos.ActualStart.Add(s.Duration.Duration()) // durations inviolable (invariant 3)
		occupied[pos.AssignedResource] = append(occupied[pos.AssignedResource], span{pos.ActualStart, end, id})
	}

	// ACTIVE slots are untouched (invariant 5): absent from the queue, and their
	// occupancy is part of the no-overlap check.
	for _, s := range in.Slots {
		if !s.Status.IsPinned() {
			continue
		}
		_, present := res.Queue[s.ID]
		assert.Falsef(t, present, "active slot %q must not be in the queue", s.ID)
		occupied[s.AssignedResource] = append(occupied[s.AssignedResource], span{s.ActualStart, s.ProjectedEnd, s.ID})
	}

	// No two slots overlap on one resource (invariant 1); different resources may.
	for rid, spans := range occupied {
		sort.Slice(spans, func(i, j int) bool { return spans[i].start.Before(spans[j].start) })
		for i := 1; i < len(spans); i++ {
			assert.Falsef(t, spans[i].start.Before(spans[i-1].end),
				"overlap on resource %q: %q ends %s but %q starts %s",
				rid, spans[i-1].id, spans[i-1].end, spans[i].id, spans[i].start)
		}
	}
}

// TestRescheduleValidation exercises the input guard Reschedule runs first.
func TestRescheduleValidation(t *testing.T) {
	eng := New(Config{ClockInGrace: 15})
	foreignPool := sched("x", 1, 0, 60, 0, -1)
	foreignPool.ResourcePoolID = "other-pool"
	noResource := active("y", 1, "r1", 0, 60)
	noResource.AssignedResource = ""

	cases := []struct {
		name string
		in   dynamicqueue.Input
	}{
		{"no resources", dynamicqueue.Input{ResourcePoolID: pool, Now: base, Slots: []dynamicqueue.Slot{sched("a", 1, 0, 60, 0, -1)}}},
		{"duplicate priority", dynamicqueue.Input{ResourcePoolID: pool, Resources: resources("r1"), Now: base, Slots: []dynamicqueue.Slot{sched("a", 1, 0, 60, 0, -1), sched("b", 1, 0, 30, 0, -1)}}},
		{"non-positive duration", dynamicqueue.Input{ResourcePoolID: pool, Resources: resources("r1"), Now: base, Slots: []dynamicqueue.Slot{sched("a", 1, 0, 0, 0, -1)}}},
		{"foreign pool slot", dynamicqueue.Input{ResourcePoolID: pool, Resources: resources("r1"), Now: base, Slots: []dynamicqueue.Slot{foreignPool}}},
		{"active without resource", dynamicqueue.Input{ResourcePoolID: pool, Resources: resources("r1"), Now: base, Slots: []dynamicqueue.Slot{noResource}}},
		{"active projected end before now", dynamicqueue.Input{ResourcePoolID: pool, Resources: resources("r1"), Now: base, Slots: []dynamicqueue.Slot{active("z", 1, "r1", -60, -1)}}},
		{"two active slots on one resource", dynamicqueue.Input{ResourcePoolID: pool, Resources: resources("r1"), Now: base, Slots: []dynamicqueue.Slot{active("a", 1, "r1", 0, 60), active("b", 2, "r1", 0, 90)}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := eng.Reschedule(c.in)
			require.Error(t, err)
		})
	}
}

// TestRescheduleOverrunFreesAtNow checks the projectedEnd == now boundary: an
// overrunning ACTIVE slot re-projected to "frees imminently" leaves its resource
// free from now, so the slot behind it is placed at the now floor (the overrun
// behaviour, §6). A projection strictly before now is rejected (see the validation
// case above); exactly now is accepted.
func TestRescheduleOverrunFreesAtNow(t *testing.T) {
	eng := New(Config{ClockInGrace: 15})
	// a overran: actual start 60m ago, projected end re-set to now (offset 0).
	// b is behind a on the only resource, desired 30m ago, no earliness.
	in := dynamicqueue.Input{
		ResourcePoolID: pool,
		Resources:      resources("r1"),
		Now:            base,
		Slots:          []dynamicqueue.Slot{active("a", 1, "r1", -60, 0), sched("b", 2, -30, 60, 0, -1)},
	}
	res, err := eng.Reschedule(in)
	require.NoError(t, err)
	pos, ok := res.Queue["b"]
	require.True(t, ok)
	assert.True(t, pos.ActualStart.Equal(base), "b should be placed at the now floor once a frees at now")
	assert.Equal(t, dynamicqueue.ResourceID("r1"), pos.AssignedResource)
}
