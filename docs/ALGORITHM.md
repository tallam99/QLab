# Quelab — Scheduling Engine Specification

> This is the **schema-of-record for the core scheduling logic**. It is written
> *before* any engine code (see `PLAN.md`, Phase 6, which only *implements* this
> document). The goal is to surface and settle the hard problems on paper, where
> they're cheap to change, rather than discovering them in a half-built handler.
>
> Audience note: this is pure algorithm/domain reasoning — no frontend, no infra.
> It is deliberately implementation-agnostic (no Go, no SQL) so it can be ported,
> argued with, and unit-tested in isolation.

---

## 0. Why this engine is the whole product

A booking calendar is a solved problem. The differentiator is what happens when
reality diverges from the plan: someone runs 20 minutes over, someone cancels,
someone finishes early, a bench frees up. Quelab's job is to **continuously
re-flow the queue across the available benches to keep them maximally used**,
within the flexibility each person declared, while never harming anyone ahead of
them in line and never interrupting an experiment already in progress.

Everything else in the system (auth, API, UI, notifications) exists to feed this
engine inputs and broadcast its outputs.

---

## 1. Domain model

The engine operates on **one equipment pool within one lab** — a set of
interchangeable **benches** (e.g. all the ventilation hoods) and the single
priority-ordered queue of people waiting to use one. Cross-pool and cross-lab
concerns never enter here.

### 1.1 Slot

| Field         | Type             | Meaning |
|---------------|------------------|---------|
| `id`          | opaque id        | Stable identity; deterministic tie-breaker. |
| `user_id`     | opaque id        | Who booked it. Opaque label to the engine. |
| `lab_id`      | opaque id        | All slots in one engine call share this. |
| `pool` / `equipment` | opaque id | The interchangeable bench pool (see §1.4). The *specific* bench is assigned by the scheduler, not at booking (⚠️ data-model decision, §10). |
| `priority`    | ordered key      | Position in line. Lower = ahead. Derived from booking order (and, future, max-priority — §8.3). |
| `window`      | minutes (≥ 0)    | **Forward start-time flexibility.** See §2. |
| `winStart`    | instant          | Earliest acceptable start. Equals the booked start initially; **ratchets later** when forced (§2). The band is `[winStart, winStart + window]`. |
| `duration`    | minutes (> 0)    | Fixed. **Never changed by the engine** (note: no compression — §2). |
| `actualStart` | instant          | Where the current schedule places this slot. Output of the engine. |
| `assignedBench` | opaque id (nullable) | Which bench the schedule put it on (paired with `actualStart`). Output of the engine; provisional until clock-in, fixed once `ACTIVE`. |
| `status`      | enum             | See §1.2. |
| `note`        | text             | Opaque to the engine. |

Occupancy is always `[actualStart, actualStart + duration]` — full duration,
always.

### 1.2 Status semantics

- **`SCHEDULED`** — a future booking the engine may place anywhere in its band.
- **`ACTIVE`** — someone has clocked in; running *now* on a specific bench.
  **Pinned**: the engine never moves or interrupts it. Its projected end seeds the
  reschedule.
- **`COMPLETE`** — finished (on time, early, or over). Immutable history.
- **`CANCELLED`** — withdrawn by the user. Immutable history.
- **`NO_SHOW`** — auto-applied when the clock-in grace lapses (§2.3). Behaves like
  a cancellation for scheduling (frees the slot, triggers a reschedule) but is
  recorded distinctly for analytics. The user must rebook.

### 1.3 Priority order vs. execution order — reordering is allowed

There are **two distinct orderings**:

- **Priority order** (the *queue*): who is ahead of whom. Stable; determined by
  booking order (future: max-priority slots jump to the front — §8.3). This is the
  order the scheduler *processes* slots in.
- **Execution order** (the *schedule*): the actual `actualStart` times across
  benches. This **may differ** from priority order — a slot further back in line
  may end up running *earlier* in clock time.

**The rule (note 1): reordering in execution time is allowed, but never at the
expense of anyone ahead in priority.** A lower-priority slot may be promoted to run
earlier **only into capacity that no higher-priority slot can or will use** —
filling a gap the higher-priority slot couldn't fit into, or a bench it wasn't
going to take. This falls out *for free* from processing slots in priority order
(§5): each higher-priority slot claims its place first; lower-priority slots can
only take what's left; so no promotion can ever delay someone ahead.

> Worked through: two fixed slots A and B on a bench with a 30-min gap between
> them. C (1 hour, higher priority) can't fit the gap, so it's placed after B. D
> (30 min, lower priority) *does* fit, so it slots into the gap — running before C
> in clock time, but not at C's expense (C was always going to run after B). If A's
> projected end grows so the gap shrinks below 30 min, the next reschedule no longer
> fits D there and places D after C again. See §7.3.

### 1.4 Benches and fan-out (note 3)

A pool has **one or more interchangeable benches**. The queue is single-file, but
**fans out across benches based on availability** — like one line feeding several
identical machines. Consequences:

- **The no-overlap invariant is *per bench*** (§4): two slots may overlap in clock
  time **iff they're on different benches**.
- A delay on one bench does **not** necessarily delay the next person — if another
  bench is free within their window, they go there (§7.2). Free benches are a
  primary absorber of delay (the other is idle gaps).
- The engine assigns each slot to a specific bench as part of scheduling; the
  assignment is provisional until the user clocks in.

---

## 2. The `window` — central modeling decision  ⚠️ REVIEW THIS

`window` is "minutes of flexibility." Per **note 2**, flexibility is **purely
about *when a slot starts*, never about how long it runs**:

> **People always get exactly the duration they booked, starting whenever they
> actually start. The engine never shortens a session.**

So a slot's `window` defines a **forward band of acceptable start times**,
`[winStart, winStart + window]`, within which the scheduler may place `actualStart`.

### 2.1 What "absorb `min(delta, window)`" means now (reinterpreted)

The primer's phrase "each slot absorbs `min(delta, window)` then passes the rest"
**no longer means compression.** With durations inviolable, bench *time* still
propagates down a bench — what the window absorbs is **disruption / the need to
re-commit**, not time:

- If a delay pushes a slot's start to a later point **still inside its band**
  (`≤ winStart + window`), the slot is silently accommodated: same `winStart`, no
  re-commitment, no disruptive notification. It "absorbed" the delay.
- If the push exceeds the band, the **band ratchets** (§2.2): the slot is
  re-committed to a later time and **notified**. That's the part it "passes on."

The actual absorption of *clock time* — the thing that stops the next person being
delayed at all — comes from **gaps and free benches** (§1.4), not from windows.

### 2.2 Ratcheting (note 2): "move forward, but not back unless pushed again"

- A slot may be **pulled forward** (earlier) within its band — down to `winStart` —
  to fill freed time. It is never pulled earlier than `winStart` (the user isn't
  available before then — §2.3).
- A slot is **pushed back** (later) only when forced: a higher-priority slot or an
  overrun leaves it no feasible start within `[winStart, winStart + window]`. When
  that happens, **the whole band shifts later to follow the slot** — `winStart`
  ratchets up to the new `actualStart`. Same width, later position.
- After ratcheting, the slot can again move *forward* within the **new** band (down
  to the new `winStart`) if a gap opens, but it will **not** drift back to its
  original time on its own — only another forced push moves it later again.

### 2.3 The availability contract & no-show grace (note 6)

The deal with users, which the engine relies on:

- **A user is available at any point within their current band** `[winStart,
  winStart + window]`. So the scheduler may place them anywhere in it (including
  pulling them forward) without asking.
- **A user must clock in within `CLOCK_IN_GRACE` (= 15 min) of their placed
  `actualStart`.** If they don't, the slot becomes `NO_SHOW`, frees its place, and
  triggers a reschedule (the people behind flow forward). The user rebooks.

This is the ethos: **flexible usage tracking, not rigid reservation.** The system
optimizes for benches being used; it does not police who deserves what.

### 2.4 `window == 0` means **rigid**, not **anchored**

A zero-window slot has a zero-width band `[winStart, winStart]`:

- It can start *only* exactly at `winStart`. Any disturbance that prevents that
  immediately ratchets it (re-commit + notify) — it has **zero silent tolerance**.
- It never opportunistically moves and can only fill a gap that begins exactly at
  `winStart` and is at least `duration` long.
- When it is itself pushed, it occupies its bench for the full duration and so
  pushes whoever is behind it on that bench — "fixed pushes fixed."

> It does **not** mean "pinned to 14:00 forever, immovable." Per **note 5** there
> are **no wall-clock anchors, ever** — see §8.

---

## 3. Guiding principles (the engine's value function)

In priority order of importance:

1. **Never interrupt an in-progress experiment.** `ACTIVE` slots are pinned. An
   experiment cannot be stopped, full stop — this is the design ethos (note 5).
2. **Never harm anyone ahead in priority** (note 1, §1.3).
3. **Maximize bench usage** (note 4). A bench sitting idle while someone behind
   could productively fill it is the failure mode to avoid — even at the cost of
   adherence to the original schedule. If an optimistic gap-filler is wrong and
   overruns, that pushes the people behind them; the system accepts this and stays
   **agnostic about politeness** — the lab enforces its own norms socially.
4. **Minimize disruption** to already-committed starts (don't ratchet a band you
   didn't have to).

There is no notion of the schedule "failing." See §8.

---

## 4. Invariants (must hold after every engine call)

1. **No per-bench overlap:** on any single bench, no two slots' `[actualStart,
   actualStart + duration]` intervals overlap. (Different benches may overlap
   freely — §1.4.)
2. **Priority respected:** no slot is placed in a way that delays a higher-priority
   slot relative to the schedule that higher-priority slot would get on its own.
3. **Durations inviolable:** every slot runs for exactly its booked `duration`.
4. **Forward-only earliness:** `actualStart ≥ winStart` for every slot, and
   `winStart` is monotonically non-decreasing across reschedules (it ratchets later,
   never earlier).
5. **ACTIVE/history immutable:** `ACTIVE`, `COMPLETE`, `CANCELLED`, `NO_SHOW` slots
   are never moved.
6. **Determinism:** identical input ⇒ identical output. Processing order is
   `(priority, id)`; bench tie-breaks by bench id.
7. **No time travel:** the caller supplies `now`; no `SCHEDULED` slot is placed to
   start in the past.

The engine should **assert** these on its output in tests (and cheaply in prod) —
a violation is a bug, not a user-facing state.

---

## 5. The scheduling algorithm — one operation

The corrected model **unifies cascade and pull-forward into a single reschedule**:
given the current world state, recompute the placement of all `SCHEDULED` slots.
It is run after every event (§6); the difference between "a delay cascaded" and "a
gap pulled people forward" is just which inputs changed.

It is a **greedy, priority-ordered, multi-bench list scheduler with gap-fill.**

```
function reschedule(slots, benches, now):
    # 1. Seed bench availability from pinned ACTIVE slots (and history on each bench).
    #    free[b] = ordered list of free intervals [from, to) per bench b,
    #    with ACTIVE/immutable occupancy carved out; to may be +infinity.
    freeIntervals = buildFreeIntervals(benches, slots, now)

    # 2. Place SCHEDULED slots in PRIORITY order. Higher priority claims first,
    #    so a lower-priority slot can never displace it (invariant #2).
    for slot in slots.filter(SCHEDULED).sortBy(priority, id):
        earliest = max(slot.winStart, now)
        # find the earliest feasible start across ALL benches, allowing backfill
        # into earlier-but-short gaps the slot actually fits:
        best = argmin over (b, interval in freeIntervals[b])
                 of  t = max(interval.from, earliest)
                 subject to  t + slot.duration <= interval.to
                 tie-break: smaller t, then bench id
        slot.actualStart = best.t
        slot.bench = best.b
        carve [best.t, best.t + slot.duration) out of freeIntervals[best.b]

        # 3. Ratchet / notify bookkeeping (§2.2):
        if slot.actualStart > slot.winStart + slot.window:   # forced past the band
            slot.winStart = slot.actualStart                 # band travels with it
            mark slot as RE-COMMITTED (notify: new start)
        # else: silently accommodated within band; winStart unchanged

    return slots
```

### 5.1 Why this satisfies the principles

- **Priority order of processing** ⇒ invariant #2 and the §1.3 reordering rule come
  for free: a slot only ever takes capacity left after everyone ahead is placed.
- **`argmin t` with backfill into any interval the slot fits** ⇒ gap-fill and
  fan-out (principle 3 in §3; §1.4): a short slot drops into a short gap; a slot
  whose own bench is busy hops to a free one.
- **`earliest = max(winStart, now)`** ⇒ forward-only earliness (invariant #4) and
  the availability contract (§2.3): nobody is pulled before they're available.
- Running it after *every* event ⇒ both "push back on overrun" and "pull forward on
  early-finish/cancel" without two code paths.

### 5.2 Cost note

Naive `argmin` over all free intervals is `O(slots × benches × intervals)`. For a
15-person lab with a handful of benches this is trivial. If a pool ever grows large,
revisit with a per-bench earliest-fit index — flagged, not built.

---

## 6. Events that trigger a reschedule

Each event mutates state, then calls `reschedule`:

| Event | State change | Typical effect |
|-------|--------------|----------------|
| **Book / create** | insert a `SCHEDULED` slot at its priority | placed into the best gap/bench within its band |
| **Clock in** | `SCHEDULED → ACTIVE`, pin to a bench | that bench's availability is fixed to its projected end |
| **Overrun** (active past its scheduled end) | active slot's projected end moves later | people behind on that bench shift; some may hop benches; bands ratchet only when pushed past window |
| **Clock out / early finish** | `ACTIVE → COMPLETE`; bench frees (possibly early) | people pull forward to fill the freed time, down to their `winStart` |
| **Cancel** | `SCHEDULED/ACTIVE → CANCELLED`; place frees | pull-forward to fill |
| **No-show** | grace lapsed → `SCHEDULED → NO_SHOW`; place frees | pull-forward to fill (note 5/6) |

> **Overrun detection (⚠️ REVIEW):** recompute on a *live projection* (continuously,
> as an active slot crosses its scheduled end, so downstream ETAs stay honest) vs.
> only *settle on clock-out* (simpler). Recommend **settle on clock-out + one
> projected recompute when the active slot crosses its scheduled end** (so people
> behind get an early warning), and revisit.

---

## 7. Worked examples

Times are `HH:MM`; "band" is `[winStart, winStart+window]`.

### 7.1 Single bench — delay within window (silent) vs. past it (ratchet+notify)

```
Bench1.  A  ACTIVE 09:00 dur 60  → overruns, projected end 10:20  (Δ20)
         B  09:?? booked 10:00 dur 60  window 30  → band [10:00, 10:30]
         C  booked 11:00 dur 30  window 0   → band [11:00, 11:00]  (rigid)
```
- Bench1 free from **10:20**.
- **B:** earliest 10:00; earliest feasible on Bench1 is 10:20. `10:20 ≤ 10:30` →
  **within band → silent**, `winStart` stays 10:00. B runs 10:20–11:20.
- **C:** earliest 11:00; Bench1 free at 11:20 → `actualStart 11:20`. `11:20 >
  11:00` (band width 0) → **ratchet**: `winStart := 11:20`, **notify**. C runs
  11:20–11:50.

Takeaway: time propagated to both (no compression), but B absorbed it silently
within its window; only C (rigid) had to re-commit. No failure flag anywhere.

### 7.2 Two benches — a free bench absorbs the delay entirely

```
Bench1.  A  ACTIVE 09:00 dur 60  → overruns to 10:20
Bench2.  (free)
         B  booked 10:00 dur 60  window 30
```
- **B:** earliest 10:00; Bench2 is free at/ before 10:00 → placed on **Bench2 at
  10:00**. The overrun on Bench1 didn't touch B at all. Fan-out absorbed it.

### 7.3 Gap-fill reorder, and its undo (note 1)

```
Bench1.  A  fixed 09:00 dur 60  (09:00–10:00)
         B  fixed 10:30 dur 60  (10:30–11:30)
         => idle gap on Bench1: 10:00–10:30  (30 min)
Queue priority:  C (dur 60) ahead of  D (dur 30); both bands overlap the gap.
```
- Process in priority order: A, B pinned/placed. **C** (60 min): the 30-min gap
  doesn't fit, so C is placed after B → **11:30**. **D** (30 min): the gap *fits* →
  placed at **10:00–10:30**, running before C in clock time but not at C's expense.
- **Now A's projected end grows to 10:15** (A overruns). Reschedule: gap is now
  10:15–10:30 (15 min). C still → 11:30. **D** (30 min) no longer fits the 15-min
  gap → placed after C → **12:30**. D moved back behind C, exactly as required.
  (If D had already clocked in during the gap, it'd be `ACTIVE` and pinned — never
  interrupted; this reasoning is pre-clock-in.)

### 7.4 Pull-forward within the (possibly ratcheted) band

```
Bench1.  A  ACTIVE 09:00 dur 60  → finishes ON TIME at 10:00
         B  currently placed at 10:25 (a prior projection had pushed it within band)
            booked 10:00 dur 60  window 30  → band [10:00, 10:30], winStart 10:00
```
- A frees Bench1 at 10:00. Reschedule: **B** earliest = winStart 10:00 → placed at
  **10:00** (pulled forward from 10:25, within its band). Full duration.
- Contrast: if B had earlier been *ratcheted* to `winStart 10:30` (forced past its
  band by a real overrun), it would pull forward only to **10:30**, never back to
  10:00 — "forward, not back" (§2.2).

### 7.5 No-show grace re-flows the queue (notes 5, 6)

```
Bench1.  B  placed 10:00 dur 60   — but B never clocks in
         C  placed 11:00 dur 60  window 30
```
- At **10:15** (`CLOCK_IN_GRACE`), B is marked **`NO_SHOW`**; its place frees.
  Reschedule: **C** earliest = winStart (say 10:30 if booked then) → pulled forward
  to fill. B's owner must rebook. No flag, no human escalation required.

---

## 8. There is no "unresolvable" — resolution model (replaces failure flags)

Per **note 5**, the schedule **never overflows and is never infeasible.** The engine
always produces a valid placement by pushing slots later as needed. The old
`OVERFLOW` / `INFEASIBLE` result states are **deleted**.

### 8.1 "Pushed too far" is a human decision, not an engine error

If cascading pushes someone's start to, say, midnight, the engine still schedules
it. Resolution is entirely human/contractual:
- the user **cancels** (frees the place → reschedule), or
- the user **doesn't clock in** within grace → **`NO_SHOW`** (frees the place →
  reschedule).

Either way the queue re-flows and bench usage stays maximized. It is the user's job
to reschedule an experiment that got pushed beyond what's useful to them.

### 8.2 No wall-clock anchors — ever

A pure wall-clock anchor ("this MUST start at 14:00, immovable") contradicts the
founding premise that **experiments necessarily run over** — so it is permanently
out of scope. `window == 0` (rigid) is as inflexible as a slot gets, and even a
rigid slot slides when the queue ahead of it slides.

### 8.3 Future: max-priority slots (the only thing that "jumps the line")

A planned feature: a **max-priority** slot that, whenever created, goes to the
**front of the priority queue** — so it's placed first on the next reschedule and
pushes others back. It is *priority*, not a wall-clock anchor:
- it still **never interrupts an `ACTIVE` slot** (can't stop a running experiment);
- it integrates with zero new machinery — it's just the lowest `priority` key, so
  §5 already handles it. Invariant #2 ("never harm those ahead") holds *by
  definition of priority*: a max-priority slot **is** ahead.

---

## 9. Edge cases the implementation must cover

| Case | Expected behavior |
|------|-------------------|
| Empty queue / single slot | No-op placement. |
| All benches busy with `ACTIVE` | `SCHEDULED` slots queue behind projected ends; nobody interrupted. |
| Slot fits no current gap | Placed at the earliest open-ended interval (after the last occupant on the best bench). |
| Delay within window | Silent placement, no ratchet, no notify (§7.1). |
| Delay past window | Ratchet `winStart`, notify (§7.1). |
| Early finish before everyone's `winStart` | Bench idles until the first available person — acceptable (nobody is available to pull forward). |
| Gap-fill promotes a lower-priority slot | Allowed iff it doesn't delay anyone ahead (guaranteed by priority-order processing). |
| Promotion undone when a gap shrinks | Next reschedule re-places it behind (§7.3). |
| Active slot overruns repeatedly | Each event reschedules from current state; durations never shrink (no floor needed). |
| No-show | `NO_SHOW`, free place, reschedule (§7.5). |
| Cancel the active slot | `CANCELLED`, free its bench from `now`, reschedule. |
| Multi-bench, uneven durations | Backfill `argmin` handles it; assert per-bench no-overlap. |

---

## 10. Implementation contract (how Phase 6 / Phase 7 must use this)

- **One pure function: `reschedule(slots, benches, now) → slots`.** No DB, no HTTP,
  no clock reads inside; the caller injects `now`, projected active-end times, and
  `CLOCK_IN_GRACE`. Cascade and pull-forward are *the same call*.
- **Convert at the edges.** proto ⇄ domain and DB-row ⇄ domain conversions live in
  the handler/repository, never inside the engine.
- **One transaction per event.** A reschedule touches many rows; the caller loads
  the pool's open slots `FOR UPDATE`, runs the engine, and persists the whole result
  atomically, so no observer sees a half-shifted queue (PLAN Phase 7).
- **Re-cascade baseline (⚠️ REVIEW):** each event reschedules from the slots'
  *current* persisted state. With durations inviolable and `winStart` ratcheting
  monotonically, there is **no compounding-shrinkage risk** (the old `MIN_DURATION`
  concern is gone). The thing to watch instead is **band thrash** — a slot silently
  nudged within its window repeatedly; cap notifications, not placements.
- **Bench-assignment data model (⚠️ REVIEW, §1.1/§1.4):** dynamic fan-out implies a
  slot's *specific* bench is chosen by the engine (near clock-in), not fixed at
  booking. The schema therefore needs an **equipment *pool*** concept and a nullable
  assigned-bench, not a hard `equipment_id` at creation. This affects `PLAN.md`
  Phase 4 — see the sync note when reviewing.
- **v1 scope (⚠️ REVIEW):** full multi-bench gap-fill is specified above. If the
  first lab has a **single** hood, the same algorithm degenerates correctly to one
  bench — so v1 can ship the general engine with `benches == 1` and grow to N with
  no rewrite. Recommend doing exactly that.

---

## 11. Test matrix (drives the Phase 6 table-driven `testify` suite)

Each row is one table-test case; expected output is the full resolved schedule
(per-slot `actualStart`, `bench`, ratcheted `winStart`, notify-flag).

**Single bench:**
1. Empty queue / single active slot — no-op.
2. Delay absorbed within a window — silent, no ratchet (§7.1 / B).
3. Delay past a window — ratchet + notify (§7.1 / C).
4. Rigid slot (`window 0`) — ratchets on any disturbance.
5. Chain of slots, partial absorption then propagation — verify each band-shift.
6. Early finish pulls a slot forward within its band (§7.4).
7. Ratcheted slot pulls forward only to its new `winStart`, not the original.

**Multi-bench:**
8. Free bench absorbs a delay (slot hops benches, no delay) (§7.2).
9. Gap-fill promotes a shorter lower-priority slot (§7.3 first half).
10. Promotion undone when the gap shrinks (§7.3 second half).
11. Uneven durations across benches — per-bench no-overlap holds.

**Lifecycle:**
12. No-show after grace re-flows the queue (§7.5).
13. Cancel a scheduled slot — pull-forward.
14. Cancel/clock-out the active slot — bench frees, reschedule.
15. Booking inserts into the best gap within its band.
16. Max-priority insert (when built) — goes to front, never interrupts active.

**Invariants (assert on every case):** per-bench no-overlap; priority respected;
durations unchanged; `actualStart ≥ winStart` and `winStart` monotonic; ACTIVE/
history untouched; determinism (run twice, compare).

---

## 12. Open decisions for review (consolidated)

Several earlier decisions are now **settled** by your notes (no compression; no
overflow/infeasible; no wall-clock anchors; auto-apply within window via the
availability contract; `CLOCK_IN_GRACE = 15 min`). Remaining items to confirm:

1. **§2 / §2.2** — `window` = *forward* band only (`[winStart, winStart+window]`),
   earliness bounded by `winStart`, ratcheting later when forced. Confirm this is
   forward-only (not symmetric around the booked start).
2. **§1.1 / §10** — **dynamic bench assignment** ⇒ an *equipment pool* in the data
   model and a bench chosen near clock-in (not a fixed `equipment_id` at booking).
   Confirm; this drives a `PLAN.md` Phase 4 change.
3. **§10** — ship the **general N-bench engine with `benches == 1`** for the first
   lab rather than a single-bench special case. Confirm.
4. **§6** — overrun handling = *settle on clock-out* + one projected recompute when
   an active slot crosses its scheduled end. Confirm.
5. **§2.3** — `CLOCK_IN_GRACE = 15 min` (note 6) and `NO_SHOW` as a distinct status.
   Confirm the value and that no-show auto-frees + reschedules.
6. **§8.3** — max-priority slots are a post-MVP feature implemented purely as the
   lowest `priority` key (no new engine machinery, never interrupts active).
   Confirm the deferral.
