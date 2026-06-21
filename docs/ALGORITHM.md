# QLab — Scheduling Engine Specification

> This is the **schema-of-record for the core scheduling logic**. It is written
> *before* any engine code (see `docs/PLAN.md`, Phase 4, which only *implements* this
> document). The goal is to surface and settle the hard problems on paper, where
> they're cheap to change, rather than discovering them in a half-built handler.
>
> The implementing package is **`backend/internal/dynamicqueue`** (interface +
> domain types) with a **`basic`** subpackage as the first algorithm.
>
> Audience note: this is pure algorithm/domain reasoning — no frontend, no infra.
> It is deliberately implementation-agnostic (no Go, no SQL) so it can be ported,
> argued with, and unit-tested in isolation.

---

## 0. Why this engine is the whole product

A booking calendar is a solved problem. The differentiator is what happens when
reality diverges from the plan: someone runs 20 minutes over, someone cancels,
someone finishes early, a resource frees up. QLab's job is to **continuously
re-flow the queue across the available resources to keep them maximally used**,
pulling people earlier when capacity opens within the earliness they allowed,
while never harming anyone ahead of them in line and never interrupting work
already in progress.

Everything else in the system (auth, API, UI, notifications) exists to feed this
engine inputs and broadcast its outputs.

---

## 1. Domain model

The engine operates on **one resource pool within one lab** — a set of
interchangeable **resources** (e.g. all the vent hoods) and the single
priority-ordered queue of people waiting to use one. Cross-pool and cross-lab
concerns never enter here.

### 1.1 Slot

| Field         | Type             | Meaning |
|---------------|------------------|---------|
| `id`          | opaque id        | Stable identity. **Not** the ordering key (§1.3). |
| `userID`      | opaque id        | Who booked it. Opaque to the engine. |
| `labID`       | opaque id        | All slots in one engine call share this. |
| `pool`        | opaque id        | The interchangeable resource pool (see §1.4). The *specific* resource is assigned by the engine, not at booking (§10). |
| `slotPriority`| ordered key      | Position in line: lower = ahead. A **unique total order** across the pool's open slots and the **sole** processing/tie-break key (§1.3, §4). |
| `desiredStart`| instant          | The booked/intended start. The reference point for earliness (§2). |
| `lookahead`   | minutes (≥ 0)    | How far **before** `desiredStart` the engine may pull the slot. See §2. |
| `duration`    | minutes (> 0)    | Fixed. **Never changed by the engine** (no compression — §2). |
| `committedStart` | instant (nullable) | The start the user was last notified of. The reference for re-commit (§2.2) and no-show (§2.3). Null until first committed. |
| `actualStart` | instant          | Where the current schedule places this slot. **Output** of the engine. |
| `assignedResource` | opaque id (nullable) | Which resource the schedule put it on. **Output**; provisional until clock-in, fixed once `ACTIVE`. |
| `status`      | enum             | See §1.2. |
| `note`        | text             | Opaque to the engine. |

Occupancy is always `[actualStart, actualStart + duration]` — full duration,
always.

**Earliest allowed start** of a slot is `desiredStart − lookahead` (and never
before `now`). The engine places `actualStart` at the earliest feasible instant
at or after that floor (§5).

### 1.2 Status semantics

The engine's world is just two live states, plus one outcome it emits:

- **`SCHEDULED`** — a future booking the engine may place anywhere at or after its
  earliest allowed start.
- **`ACTIVE`** — someone has clocked in; running *now* on a specific resource.
  **Pinned**: the engine never moves or interrupts it. Its projected end seeds the
  reschedule.
- **`NO_SHOW`** — an **output**: the engine marks a `SCHEDULED` slot whose clock-in
  grace has lapsed (§2.3). It frees the slot's place (triggering re-flow) and is
  recorded distinctly for analytics. The user must rebook.

**`COMPLETE` / `CANCELLED`** are settled history owned by the service lifecycle.
The engine **never sees them** — the caller filters history out before the call,
so the engine always reasons about the current state of the world, not its past
(§10).

### 1.3 Priority order vs. execution order — reordering is allowed

There are **two distinct orderings**:

- **Priority order** (the *queue*): who is ahead of whom, given by `slotPriority`.
  It is a stable, unique total order (no two open slots tie), determined by booking
  order (future: max-priority slots take the smallest value — §8.3). This is the
  order the scheduler *processes* slots in, and `slotPriority` alone makes
  processing deterministic — `id` is never used to break ties (§4).
- **Execution order** (the *schedule*): the actual `actualStart` times across
  resources. This **may differ** from priority order — a slot further back in line
  may end up running *earlier* in clock time.

**The rule (note 1): reordering in execution time is allowed, but never at the
expense of anyone ahead in priority.** A lower-priority slot may be promoted to run
earlier **only into capacity that no higher-priority slot can or will use** —
filling a gap the higher-priority slot couldn't fit into, or a resource it wasn't
going to take. This falls out *for free* from processing slots in priority order
(§5): each higher-priority slot claims its place first; lower-priority slots can
only take what's left; so no promotion can ever delay someone ahead.

> Worked through: two placed slots A and B on a resource with a 30-min gap between
> them. C (1 hour, higher priority) can't fit the gap, so it's placed after B. D
> (30 min, lower priority) *does* fit, so it slots into the gap — running before C
> in clock time, but not at C's expense (C was always going to run after B). A slot
> promoted into a gap keeps its own `slotPriority`, so if A's projected end grows
> and the gap shrinks below 30 min, the next reschedule no longer fits D there and
> places D after C again. See §7.3.

### 1.4 Resources and fan-out (note 3)

A pool has **one or more interchangeable resources**. The queue is single-file, but
**fans out across resources based on availability** — like one line feeding several
identical machines. Consequences:

- **The no-overlap invariant is *per resource*** (§4): two slots may overlap in
  clock time **iff they're on different resources**.
- A delay on one resource does **not** necessarily delay the next person — if
  another resource is free within their reach, they go there (§7.2). Free resources
  are a primary absorber of delay (the other is idle gaps).
- The engine assigns each slot to a specific resource as part of scheduling; the
  assignment is provisional until the user clocks in.

A resource has a **kind** identifying what it is (enumerated; the MVP ships a single
kind, the vent hood). A pool groups interchangeable resources of one kind; the
engine itself is kind-agnostic — it only needs the set of resources it may place
onto.

---

## 2. The `lookahead` — central modeling decision

`lookahead` is "minutes of earliness." Flexibility is **purely about letting a slot
start *earlier* than desired**, never about how long it runs and never about
starting later by choice:

> **People always get exactly the duration they booked. The only flexibility they
> grant is permission to start earlier than their desired time, up to `lookahead`,
> if capacity opens. The engine never shortens a session.**

So a slot's earliness band is `[desiredStart − lookahead, desiredStart]`: the
engine may pull `actualStart` anywhere into it, and prefers the earliest feasible
point (§5). A slot is only ever placed **later** than `desiredStart` when forced —
there is no upper window and no late flexibility (§2.1).

### 2.1 Greedy, not absorbing (what replaced "absorb `min(delta, window)`")

The primer's phrase "each slot absorbs `min(delta, window)` then passes the rest"
**does not describe this model.** There is no silent-tolerance band that swallows
delay. Clock time still propagates down a resource (occupancy is real), but the
thing that stops the next person being delayed is **gaps and free resources**
(§1.4), not a per-slot tolerance.

- The engine is **greedy**: it pulls each slot to the earliest feasible start at or
  after `desiredStart − lookahead`. `lookahead` is opportunistic — it lets the
  engine move someone *ahead* of their desired time to fill freed capacity.
- A slot is pushed **later** than `desiredStart` only when current occupancy leaves
  no earlier feasible start (a higher-priority slot or an active overrun took the
  space). There is **no bound** on how late — "pushed too far" is a human decision
  (§8.1).

### 2.2 No ratcheting; re-commit on any change (note 2)

Each reschedule recomputes placement from scratch. A slot's earliness floor
(`desiredStart − lookahead`) is fixed at booking and **never moves**; nothing is
sticky between runs except `now` advancing. A slot may therefore move **earlier or
later** from one reschedule to the next purely as current occupancy changes — there
is no monotonic ratchet.

- **Re-commit / notify fires whenever a slot's `actualStart` differs from its
  `committedStart`** — whether it moved earlier (good news: "you can start sooner")
  or later ("you've been pushed"). That is the single notify signal the engine
  emits per slot (§5). A brand-new slot (null `committedStart`) being placed for the
  first time is also a commit.

### 2.3 The availability contract & no-show grace (note 6)

The deal with users, which the engine relies on:

- **A user is available from `desiredStart − lookahead` onward.** By granting a
  `lookahead` they accept being pulled that much earlier; the scheduler may place
  them anywhere at or after that floor without asking.
- **A user must clock in within `CLOCK_IN_GRACE` of their `committedStart`.** The
  grace period is **injected by the service from configuration** — the engine reads
  it as a parameter, never a hardcoded constant (§10). If a `SCHEDULED` slot's
  `committedStart + CLOCK_IN_GRACE` is before `now` and it hasn't clocked in, the
  **engine itself** marks it `NO_SHOW`, frees its place, and the others flow forward.
  The user rebooks.

This is the ethos: **flexible usage tracking, not rigid reservation.** The system
optimizes for resources being used; it does not police who deserves what.

### 2.4 `lookahead == 0` means **no earliness**, not **anchored**

A zero-lookahead slot has a zero-width earliness band `[desiredStart, desiredStart]`:

- It can never be pulled earlier than `desiredStart`; it starts at `desiredStart`
  or, if forced, later.
- It still **slides later** when the queue ahead of it slides — it is not pinned to
  an exact wall-clock time. There is no "must start at exactly 14:00, immovable"
  concept anywhere (§8.2).

---

## 3. Guiding principles (the engine's value function)

In priority order of importance:

1. **Never interrupt in-progress work.** `ACTIVE` slots are pinned. Work cannot be
   stopped, full stop — this is the design ethos (note 5).
2. **Never harm anyone ahead in priority** (note 1, §1.3).
3. **Maximize resource usage** (note 4). A resource sitting idle while someone
   behind could productively fill it is the failure mode to avoid — even at the cost
   of adherence to the original schedule. If an optimistic gap-filler is wrong and
   overruns, that pushes the people behind them; the system accepts this and stays
   **agnostic about politeness** — the lab enforces its own norms socially.
4. **Minimize disruption** to already-committed starts. Because *any* change to a
   slot's start re-commits and notifies (§2.2), the engine must not move a slot it
   didn't have to: greedy-earliest placement plus determinism (§4) means identical
   occupancy yields identical placement, so slots only move when the world actually
   changed under them.

There is no notion of the schedule "failing." See §8.

---

## 4. Invariants (must hold after every engine call)

1. **No per-resource overlap:** on any single resource, no two slots'
   `[actualStart, actualStart + duration]` intervals overlap. (Different resources
   may overlap freely — §1.4.)
2. **Priority respected:** no slot is placed in a way that delays a higher-priority
   slot relative to the schedule that higher-priority slot would get on its own.
3. **Durations inviolable:** every slot runs for exactly its booked `duration`.
4. **Earliness floor:** `actualStart ≥ desiredStart − lookahead` for every placed
   slot, and `actualStart ≥ now`. (There is no monotonic ratchet — a slot may move
   earlier again on a later call.)
5. **ACTIVE untouched:** `ACTIVE` slots are never moved or reassigned. History
   (`COMPLETE`/`CANCELLED`) is never seen (§1.2).
6. **Determinism:** identical input ⇒ identical output. Processing order is
   `slotPriority` alone, which is unique — there is no secondary tie-break.
7. **No time travel:** no slot is placed to start before `now`.

The engine should **assert** these on its output in tests (this is test logic, not a
runtime path — §10); a violation is a bug, not a user-facing state.

---

## 5. The scheduling algorithm — one operation

The model **unifies cascade and pull-forward into a single reschedule**: given the
current world state, recompute the placement of all `SCHEDULED` slots. It runs after
every event (§6); the difference between "a delay cascaded" and "a gap pulled people
forward" is just which inputs changed.

It is a **greedy, priority-ordered, multi-resource list scheduler with gap-fill**,
preceded by a no-show sweep.

```
function reschedule(slots, resources, now, grace):
    # 1. No-show sweep: free slots whose grace has lapsed, before placing anyone.
    for slot in slots.filter(SCHEDULED):
        if slot.committedStart != null and now > slot.committedStart + grace:
            outcome[slot] = NO_SHOW            # freed; not placed
    open = slots.filter(SCHEDULED and outcome != NO_SHOW)

    # 2. Seed resource availability from pinned ACTIVE occupancy and now.
    #    free[r] = ordered free intervals [from, to) per resource r, with ACTIVE
    #    occupancy carved out; the tail interval is open-ended.
    free = buildFreeIntervals(resources, slots.filter(ACTIVE), now)

    # 3. Place open slots in priority order. Higher priority claims first, so a
    #    lower-priority slot can never displace it (invariant #2).
    for slot in open.sortBy(slotPriority):
        earliest = max(slot.desiredStart - slot.lookahead, now)
        # earliest feasible start across ALL resources, backfilling into any gap
        # the slot actually fits:
        best = argmin over (r, interval in free[r])
                 of  t = max(interval.from, earliest)
                 subject to  t + slot.duration <= interval.to
                 tie-break: smaller t, then resource id
        outcome[slot]         = PLACED
        slot.actualStart      = best.t
        slot.assignedResource = best.r
        carve [best.t, best.t + slot.duration) out of free[best.r]

        # 4. Re-commit bookkeeping (§2.2):
        if slot.committedStart == null or slot.actualStart != slot.committedStart:
            mark slot RE-COMMITTED (notify: new start)
    return outcomes      # PLACED (with start + resource + re-commit flag) or NO_SHOW
```

### 5.1 Why this satisfies the principles

- **Priority order of processing** ⇒ invariant #2 and the §1.3 reordering rule come
  for free: a slot only ever takes capacity left after everyone ahead is placed.
- **`argmin t` with backfill into any interval the slot fits** ⇒ gap-fill and
  fan-out (principle 3 in §3; §1.4): a short slot drops into a short gap; a slot
  whose own resource is busy hops to a free one.
- **`earliest = max(desiredStart − lookahead, now)`** ⇒ the earliness floor
  (invariant #4) and the availability contract (§2.3): nobody is pulled before their
  granted earliness, and nobody starts in the past.
- Running it after *every* event ⇒ both "push back on overrun" and "pull forward on
  early-finish/cancel/no-show" without two code paths.

### 5.2 Cost note

Naive `argmin` over all free intervals is `O(slots × resources × intervals)`. For a
15-person lab with a handful of resources this is trivial. If a pool ever grows
large, revisit with a per-resource earliest-fit index — flagged, not built.

---

## 6. Events that trigger a reschedule

Each event mutates state, then calls `reschedule`:

| Event | State change | Typical effect |
|-------|--------------|----------------|
| **Book / create** | insert a `SCHEDULED` slot at its priority | placed at the earliest feasible start at or after its earliness floor |
| **Clock in** | `SCHEDULED → ACTIVE`, pin to a resource | that resource's availability is fixed to its projected end |
| **Overrun** (active past its scheduled end) | active slot's projected end moves later | people behind on that resource shift; some may hop resources |
| **Clock out / early finish** | `ACTIVE → COMPLETE`; resource frees (possibly early) | people pull forward to fill the freed time, down to their earliness floor |
| **Cancel** | `SCHEDULED/ACTIVE → CANCELLED`; place frees | pull-forward to fill |
| **No-show** | *engine-detected* in the sweep (§5 step 1): grace lapsed → `NO_SHOW` | pull-forward to fill (note 5/6) |

> **No-show needs a trigger:** because the engine only acts when called, *something*
> must reschedule for a lapse to be noticed — a lightweight periodic check, or
> evaluating it lazily on the next event/read. Pick one in the service layer; the
> engine just needs `now` and `grace` to do the sweep (§10).
>
> **Overrun detection (⚠️ REVIEW):** recompute on a *live projection* (continuously,
> as an active slot crosses its scheduled end, so downstream ETAs stay honest) vs.
> only *settle on clock-out* (simpler). Recommend **settle on clock-out + one
> projected recompute when the active slot crosses its scheduled end** (so people
> behind get an early warning), and revisit.

---

## 7. Worked examples

Times are `HH:MM`. "Earliness floor" is `desiredStart − lookahead`.

### 7.1 Single resource — pulled early (within lookahead) vs. pushed late (forced)

```
Resource1.  A  ACTIVE 09:00 dur 60  → projected end 10:00 (on time)
            B  desired 10:30 dur 60  lookahead 30  → floor 10:00  (committed 10:30)
            C  desired 11:30 dur 30  lookahead 0   → floor 11:30
```
- Resource1 frees at **10:00**.
- **B:** floor 10:00; earliest feasible on Resource1 is 10:00 → `actualStart 10:00`,
  30 min earlier than desired. `10:00 ≠ 10:30` → **re-commit + notify** ("you can
  start at 10:00"). B runs 10:00–11:00.
- **C:** floor 11:30; Resource1 is free from 11:00 but C can't start before 11:30 →
  `actualStart 11:30`. C runs 11:30–12:00 (resource idle 11:00–11:30 — nobody is
  available earlier).

Now suppose **A overruns to 10:20**:
- **B:** floor 10:00; Resource1 free at 10:20 → `actualStart 10:20` (forced later).
  Re-commit + notify. B runs 10:20–11:20.
- **C:** floor 11:30; Resource1 free 11:20 → `actualStart 11:30`. Unchanged, no
  notify.

Takeaway: lookahead lets B run *ahead* of desired when capacity exists, and B is
pushed *later* only when the overrun forces it. Any change notifies; nothing fails.

### 7.2 Two resources — a free resource absorbs the delay entirely

```
Resource1.  A  ACTIVE 09:00 dur 60  → overruns to 10:20
Resource2.  (free)
            B  desired 10:00 dur 60  lookahead 0   → floor 10:00
```
- **B:** floor 10:00; Resource2 is free at 10:00 → placed on **Resource2 at 10:00**.
  The overrun on Resource1 didn't touch B. Fan-out absorbed it.

### 7.3 Gap-fill reorder, and its undo (note 1)

```
Resource1.  A  ACTIVE 09:00 dur 60  → projected end 10:00
            B  desired 10:30 dur 60  lookahead 0   (priority ahead of C, D)
            => placed 10:30–11:30, leaving an idle gap 10:00–10:30 (30 min)
Queue priority:  C (dur 60) ahead of  D (dur 30); both floors ≤ 10:00.
```
- Process in priority order: B placed 10:30. **C** (60 min): the 30-min gap doesn't
  fit, so C is placed after B → **11:30**. **D** (30 min): the gap *fits* → placed at
  **10:00–10:30**, running before C in clock time but not at C's expense.
- **Now A's projected end grows to 10:15** (A overruns). Reschedule: the gap is now
  10:15–10:30 (15 min). C still → 11:30. **D** (30 min) no longer fits the 15-min gap
  → placed after C → **12:30**, re-committed (start moved). D dropped back behind C,
  exactly as required, because it kept its own `slotPriority`. (If D had already
  clocked in during the gap, it'd be `ACTIVE` and pinned — never interrupted; this
  reasoning is pre-clock-in.)

### 7.4 Pull-forward, including earlier than desired

```
Resource1.  A  ACTIVE 09:00 dur 60  → finishes ON TIME at 10:00
            B  committed 10:25 (a prior projection), desired 10:30 dur 60
               lookahead 30  → floor 10:00
```
- A frees Resource1 at 10:00. Reschedule: **B** floor 10:00 → placed at **10:00**,
  pulled forward from 10:25 and even ahead of its 10:30 desired (within lookahead).
  Re-commit + notify. Full duration.
- Contrast: with `lookahead 0`, B's floor would be 10:30, so it would sit at 10:30
  and leave Resource1 idle 10:00–10:30.

### 7.5 No-show grace re-flows the queue (notes 5, 6)

```
Resource1.  B  committed 10:00 dur 60   — but B never clocks in
            C  desired 11:00 dur 60  lookahead 30  → floor 10:30
grace = 15 (injected from config)
```
- At a reschedule with `now ≥ 10:15`, B's `committedStart 10:00 + grace 15 = 10:15`
  is past → the engine marks B **`NO_SHOW`**; its place frees. **C** floor 10:30 →
  pulled forward to **10:30** to fill. B's owner must rebook. No flag, no human
  escalation required.

---

## 8. There is no "unresolvable" — resolution model

The schedule **never overflows and is never infeasible.** The engine always produces
a valid placement by pushing slots later as needed. There are no `OVERFLOW` /
`INFEASIBLE` result states.

### 8.1 "Pushed too far" is a human decision, not an engine error

If cascading pushes someone's start to, say, midnight, the engine still schedules
it. Resolution is entirely human/contractual:
- the user **cancels** (frees the place → reschedule), or
- the user **doesn't clock in** within grace → **`NO_SHOW`** (the engine frees the
  place → reschedule).

Either way the queue re-flows and resource usage stays maximized. It is the user's
job to reschedule work that got pushed beyond what's useful to them.

### 8.2 No wall-clock anchors — ever

A pure wall-clock anchor ("this MUST start at 14:00, immovable") contradicts the
founding premise that **work necessarily runs over** — so it is permanently out of
scope. `lookahead == 0` (no earliness) is as inflexible as a slot gets on the early
side, and even it slides later when the queue ahead of it slides.

### 8.3 Future: max-priority slots (the only thing that "jumps the line")

A planned feature: a **max-priority** slot that, whenever created, takes the
**smallest `slotPriority`** — so it's placed first on the next reschedule and pushes
others back. It is *priority*, not a wall-clock anchor:
- it still **never interrupts an `ACTIVE` slot** (can't stop running work);
- it integrates with zero new machinery — it's just the lowest priority key, so §5
  already handles it. Invariant #2 ("never harm those ahead") holds *by definition of
  priority*: a max-priority slot **is** ahead.

---

## 9. Edge cases the implementation must cover

| Case | Expected behavior |
|------|-------------------|
| Empty queue / single slot | No-op placement. |
| All resources busy with `ACTIVE` | `SCHEDULED` slots queue behind projected ends; nobody interrupted. |
| Slot fits no current gap | Placed at the earliest open-ended interval (after the last occupant on the best resource). |
| Capacity opens within lookahead | Slot pulled earlier than desired, down to its floor (§7.1, §7.4). |
| `lookahead == 0` | Slot never pulled before `desiredStart`; only ever pushed later (§2.4). |
| Start changed (earlier or later) | Re-commit + notify (§2.2). |
| Start unchanged | No notify. |
| Gap-fill promotes a lower-priority slot | Allowed iff it doesn't delay anyone ahead (guaranteed by priority-order processing). |
| Promotion undone when a gap shrinks | Next reschedule re-places it behind (§7.3). |
| Active slot overruns repeatedly | Each event reschedules from current state; durations never shrink. |
| No-show | Engine detects via `committedStart + grace < now`, marks `NO_SHOW`, frees the place, reschedules (§7.5). |
| Cancel the active slot | `CANCELLED` upstream; resource frees from `now`, reschedule. |
| Multi-resource, uneven durations | Backfill `argmin` handles it; assert per-resource no-overlap. |

---

## 10. Implementation contract (how Phase 4 / Phase 7 must use this)

- **One pure function: `reschedule(input) → (queue, error)`.** No DB, no HTTP, no
  clock reads inside; the caller injects `now`, projected active-end times, and the
  `CLOCK_IN_GRACE` period (the engine is **agnostic to configuration** — grace is a
  parameter wired from the service's environment, never a constant in the engine).
  Cascade and pull-forward are *the same call*.
- **The engine sees only the live world.** The caller passes `SCHEDULED` and
  `ACTIVE` slots; it filters `COMPLETE`/`CANCELLED` history out first. The engine
  emits `NO_SHOW` itself (§2.3, §5).
- **Output is per-slot outcomes**, not mutated input: each open slot resolves to
  `PLACED` (with `actualStart`, `assignedResource`, and a re-commit flag) or
  `NO_SHOW`. The caller applies these back to persisted rows and emits notifications
  for re-committed and no-showed slots.
- **Convert at the edges.** proto ⇄ domain and DB-row ⇄ domain conversions live in
  the handler/repository, never inside the engine.
- **One transaction per event.** A reschedule touches many rows; the caller loads
  the pool's open slots `FOR UPDATE`, runs the engine, and persists the whole result
  atomically, so no observer sees a half-shifted queue (PLAN Phase 7).
- **Recompute baseline:** each event reschedules from the slots' *current* persisted
  state. With durations inviolable and a fixed earliness floor there is no
  compounding-shrinkage risk. The thing to watch instead is **notification thrash** —
  a slot nudged repeatedly; cap notifications if needed, not placements.
- **Resource-assignment data model (§1.1/§1.4):** dynamic fan-out implies a slot's
  *specific* resource is chosen by the engine (near clock-in), not fixed at booking.
  The schema therefore needs a **resource *pool*** concept and a nullable
  assigned-resource, not a hard `resource_id` at creation. This affects `docs/PLAN.md`
  Phase 5 — see the sync note when reviewing.
- **Trace:** the algorithm interface returns, alongside the queue, an ordered
  **trace** of the steps it took (seed, place, gap-fill, re-commit, no-show) so any
  run can be reconstructed for debugging. Defined at the interface so every
  implementation produces it; cheap at QLab's scale (runs are tiny). If it ever grew,
  switch the accumulating trace for an injected step sink.
- **v1 scope:** full multi-resource gap-fill is specified above. If the first lab has
  a **single** resource, the same algorithm degenerates correctly to one resource —
  so v1 can ship the general engine with `resources == 1` and grow to N with no
  rewrite.

---

## 11. Test matrix (drives the Phase 4 table-driven `testify` suite)

Each row is one table-test case; expected output is the full resolved set of
outcomes (per-slot `actualStart`, `assignedResource`, re-commit flag, or `NO_SHOW`).

**Single resource:**
1. Empty queue / single active slot — no-op.
2. Capacity opens within lookahead — slot pulled earlier than desired, re-commit
   (§7.1 / B).
3. `lookahead == 0` — slot never pulled before `desiredStart` (§2.4).
4. Overrun pushes a slot later — re-commit (§7.1, second half).
5. Start unchanged across a reschedule — no re-commit.
6. Early finish pulls a slot forward, including ahead of desired (§7.4).
7. Chain of slots, occupancy propagates down the resource — verify each placement.

**Multi-resource:**
8. Free resource absorbs a delay (slot hops resources, no delay) (§7.2).
9. Gap-fill promotes a shorter lower-priority slot (§7.3 first half).
10. Promotion undone when the gap shrinks (§7.3 second half).
11. Uneven durations across resources — per-resource no-overlap holds.

**Lifecycle:**
12. No-show after grace re-flows the queue (engine-detected) (§7.5).
13. Cancel a scheduled slot (filtered out upstream) — pull-forward.
14. Clock-out / cancel the active slot — resource frees, reschedule.
15. Booking inserts at the earliest feasible start within its reach.
16. Max-priority insert (when built) — smallest `slotPriority`, never interrupts
    active.

**Invariants (assert on every case, as test logic):** per-resource no-overlap;
priority respected; durations unchanged; `actualStart ≥ desiredStart − lookahead`
and `≥ now`; ACTIVE untouched, history never present; determinism (run twice,
compare).

---

## 12. Decisions — confirmed

This spec is the schema-of-record the engine implements.

1. **§2 — `lookahead` is earliness-only.** Band `[desiredStart − lookahead,
   desiredStart]`; the engine pulls to the earliest feasible start in it and pushes
   later only when forced. There is **no late window**, **no silent-absorption
   tolerance**, and **no ratchet** — every call recomputes from scratch. ✓
2. **§2.2 — re-commit on any change.** A slot notifies whenever `actualStart`
   differs from `committedStart`, earlier or later. ✓
3. **§2.3 / §5 — the engine detects no-shows.** A `SCHEDULED` slot with
   `committedStart + CLOCK_IN_GRACE < now` is marked `NO_SHOW` by the engine. The
   grace period is injected from configuration, not hardcoded. ✓
4. **§1.3 / §4 — `slotPriority` is a unique total order** and the sole processing
   and tie-break key; `id` is identity only. ✓
5. **§1.4 — resources are generalized and carry a kind** (enumerated; MVP: vent
   hood). Equipment is modelled as a *pool* of interchangeable resources; a resource
   is chosen near clock-in, not fixed at booking. Drives the Phase 5 schema. ✓
6. **§1.2 / §10 — the engine sees only the live world.** History
   (`COMPLETE`/`CANCELLED`) is filtered out before the call; the engine reasons about
   the present, not the past. ✓
7. **§10 — one general N-resource engine.** Ship it with `resources == 1` for the
   first lab rather than a single-resource special case. ✓
8. **§8.3 — max-priority slots are post-MVP**, implemented purely as the smallest
   `slotPriority` (no new engine machinery, never interrupts an active slot). ✓
