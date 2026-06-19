# Quelab — Cascade Scheduling Engine Specification

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
someone finishes early. Quelab's job is to **automatically re-flow the queue**
within the flexibility each person declared, and to clearly flag the cases it
cannot absorb so a human can intervene.

Everything else in the system (auth, API, UI, notifications) exists to feed this
engine inputs and broadcast its outputs.

---

## 1. Domain model

The engine operates on the **queue of one piece of equipment within one lab**.
Cross-equipment and cross-lab concerns never enter here.

### 1.1 Slot

| Field         | Type             | Meaning |
|---------------|------------------|---------|
| `id`          | opaque id        | Stable identity; used as the deterministic tie-breaker. |
| `user_id`     | opaque id        | Who booked it. Engine treats it as an opaque label. |
| `equipment_id`| opaque id        | All slots in one engine call share this. |
| `lab_id`      | opaque id        | All slots in one engine call share this. |
| `start`       | instant          | Current effective start (what users see). |
| `duration`    | minutes (> 0)    | Current effective length. `end = start + duration`. |
| `window`      | minutes (≥ 0)    | **Declared flexibility.** See §2 — this is the heart of the model. |
| `status`      | enum             | `SCHEDULED` \| `ACTIVE` \| `COMPLETE` \| `CANCELLED`. |
| `note`        | text             | Opaque to the engine. |

Derived: `end(slot) = slot.start + slot.duration`.

### 1.2 Status semantics

- **`SCHEDULED`** — a future booking. The only status the engine *moves*.
- **`ACTIVE`** — someone has clocked in; this slot is running *now*. It is the
  usual **trigger** of a cascade (overrun) or pull-forward (early finish). The
  engine does not move the active slot's `start`; it reads the active slot's
  *actual/projected end* as the propagation seed.
- **`COMPLETE`** / **`CANCELLED`** — immutable history. The engine reads them only
  to skip over them; it never edits them.

### 1.3 Queue ordering

Within an engine call, slots are processed in ascending `start`, with `id` as a
deterministic tie-breaker. **The engine never reorders the queue** — it only
shifts start times (and, on delay, shrinks durations). Whoever was 3rd in line is
still 3rd in line afterward.

---

## 2. The `window` — central modeling decision  ⚠️ REVIEW THIS

`window` is "minutes of flexibility," but flexibility has a *direction*, and the
primer's phrase **"each slot absorbs `min(delta, window)` then passes the rest"**
forces a specific reading. This section states the chosen semantics explicitly
because it determines everything downstream. **If you disagree with this, change
it here first — not in code.**

### 2.1 On a delay (cascade): `window` = how much the slot will **compress**

For the delay to *dampen* as it travels down the queue (the literal "passes the
rest" — i.e. the next slot receives a *smaller* delta), a slot must be able to
give something up. The chosen mechanism: **the slot holds its end time and eats
the delay out of its own duration**, up to `window` minutes.

> Plain English: "The person before you ran 20 minutes over. You said you had 30
> minutes of flexibility, so your session just got shortened by 20 minutes —
> but it still **ends when you originally planned**, so the person after you is
> unaffected."

Concretely, when a slot is pushed later by `push` minutes:
- `absorbed = min(push, window)`
- new duration `= duration − absorbed` (floored at `MIN_DURATION`, §6)
- new end `= old end + (push − absorbed)`  → if fully absorbed, end is unchanged.
- the slot passes `push − absorbed` to the next slot.

This is the *only* model consistent with "absorb `min(delta, window)` then pass
the rest." A pure "shift everything later by the full amount" model never passes
a *reduced* delta and so contradicts the primer.

### 2.2 On a cancellation / early finish (pull-forward): `window` = how much **earlier** the slot will start

When time frees up ahead of a slot, it may move **earlier**, keeping its full
duration, by **at most `window` minutes** before its current start.

> Plain English: "The hood freed up early. You said ±30 minutes was fine, so we
> pulled your start up by 25 minutes to fill the gap. Same session length."

### 2.3 `window == 0` means **rigid**, not **anchored**  ⚠️ REVIEW THIS

A zero-window slot has *no* flexibility:
- On delay: it absorbs nothing, so it is **pushed by the full delta** (start and
  end both move) and passes the full delta onward — **a pushed rigid slot pushes
  the next slot, rigid or not.** ("Fixed pushes fixed," per the primer.)
- On pull-forward: it **cannot move earlier**, so it stays put and **blocks**
  pull-forward for everything behind it.

> **Decision flagged:** `window == 0` here means *"this session's length and
> relative position are non-negotiable, but it can still slide in absolute time
> if the queue ahead of it slides."* It does **not** mean *"pinned to 14:00 on
> the wall clock, immovable."* A true wall-clock **anchor** is a strictly stronger
> constraint that introduces genuinely *infeasible* schedules (§5.2) and is
> deferred to post-MVP. v1 has no hard anchors; every delay is representable
> (it just may overflow past the queue tail).

### 2.4 Why the asymmetry is acceptable

Delay consumes `window` as *compression* (shorter session, held end); pull-forward
consumes `window` as *earliness* (same session, earlier start). Both are honoring
the same promise the user made — "I have N minutes of give" — just in the
direction the situation demands. The user never loses more than `window` minutes
of session, and is never moved more than `window` minutes earlier than planned.

---

## 3. Invariants (must hold after every engine call)

1. **No overlap:** for consecutive scheduled slots `i, i+1`: `start_{i+1} ≥ end_i`.
2. **Order preserved:** the by-`start` ordering (with `id` tie-break) of the input
   is the ordering of the output. No reordering.
3. **Duration floor:** every slot's duration `≥ MIN_DURATION` (§6).
4. **Window bounds:** `0 ≤ window`; and `window ≤ duration` is enforced at *slot
   creation* (you can't agree to compress away more than your whole session).
5. **History immutable:** `COMPLETE` / `CANCELLED` slots are unchanged.
6. **Determinism:** identical input + event ⇒ identical output, every time.
7. **No time travel:** the caller guarantees the propagation seed (active slot's
   end, or the freed instant) is `≥ now`; the engine never produces a `start` in
   the past *relative to the seed it was given*.

The engine should **assert** these on its output in tests (and ideally cheaply in
production) — a violated invariant is a bug, not a user-facing state.

---

## 4. Algorithm 1 — Cascade on delay (overrun)

**Trigger:** an `ACTIVE` slot's actual/projected end moves *later* than its
scheduled end (someone is running over). Generalizes to any event that pushes one
slot's end later (e.g. inserting a rigid slot into busy time — §7).

**Inputs:** the equipment's slots ordered by start; the index of the trigger
slot; the trigger's new end `seedEnd`.

```
function cascadeDelay(slots, triggerIndex, seedEnd):
    result = copy(slots)
    prevEnd = seedEnd
    for i from triggerIndex+1 to last:
        if slots[i].status != SCHEDULED:
            continue                         # skip COMPLETE/CANCELLED history
        newStart = max(slots[i].start, prevEnd)   # can't start before equip is free;
                                                   # never moves earlier on a delay
        push = newStart - slots[i].start
        if push == 0:
            break                            # gap (or full upstream absorption)
                                             # swallowed the delay → queue settles
        absorbed = min(push, slots[i].window)
        newDuration = slots[i].duration - absorbed
        if newDuration < MIN_DURATION:       # duration floor (§6)
            absorbed = slots[i].duration - MIN_DURATION
            newDuration = MIN_DURATION
        newEnd = newStart + newDuration
        result[i].start = newStart
        result[i].duration = newDuration
        result[i].displaced = (newEnd > slots[i].end())   # its END moved late
        prevEnd = newEnd

    overflowMinutes = max(0, prevEnd - originalEndOfLastScheduledSlot)
    return result, overflowMinutes
```

### 4.1 Why it dampens automatically

If a slot fully absorbs (`absorbed == push`), then
`newEnd = newStart + (duration − push) = oldStart + push + duration − push = oldEnd`
— its end is unchanged, so `prevEnd` returns to the slot's original end, and the
*next* slot sees `push == 0` and the loop breaks. **Full absorption stops the
cascade.** Partial absorption (`window < push`) leaves `push − window` to
propagate as a *smaller* delta. Gaps between slots are absorbed for free by the
`max(start, prevEnd)`.

### 4.2 Result classification

- **`ABSORBED`** — `overflowMinutes == 0`. The delay was fully swallowed by gaps
  and compression; the queue tail still ends on its original plan. (Some interior
  slots may be shorter; that's expected and is what `window` is for.)
- **`OVERFLOW`** — `overflowMinutes > 0`. The delay exceeded the queue's total
  absorptive capacity; the tail now runs late by `overflowMinutes`. This is the
  v1 reading of the primer's **"unresolvable"** flag (see §5). It is *not* an
  error — it's a "humans need to know" signal: every slot with `displaced == true`
  gets a notification (see `PLAN.md` Phase 11), and the affected users may choose
  to cancel/rebook.

### 4.3 Worked example — partial absorption, then settles (`ABSORBED`)

```
A  09:00  dur 60  ACTIVE      → overruns, actual end 10:30   (delta 30)
B  10:00  dur 60  win 20  SCHEDULED
C  11:00  dur 60  win 30  SCHEDULED
D  12:00  dur 30  win 0   SCHEDULED (rigid)
```
| step | slot | newStart | push | absorbed | newDur | newEnd | note |
|------|------|----------|------|----------|--------|--------|------|
| seed |  A   |    —     |  —   |    —     |   —    | 10:30  | overran |
|  1   |  B   |  10:30   |  30  |   20     |  40    | 11:10  | shortened, end +10 |
|  2   |  C   |  11:10   |  10  |   10     |  50    | 12:00  | shortened, end back to plan |
|  3   |  D   |  12:00   |   0  |    —     |  30    | 12:00→ | push 0 → **break** |

Result: `ABSORBED` (overflow 0). B and C gave up time; **D and the tail are
untouched.** This is the engine doing its job.

### 4.4 Worked example — overflow, rigid pushed (`OVERFLOW`)

```
A  09:00  dur 60  ACTIVE      → actual end 11:00   (delta 60)
B  10:00  dur 60  win 10  SCHEDULED
C  11:00  dur 60  win 10  SCHEDULED
D  12:00  dur 30  win 0   SCHEDULED (rigid)
```
| step | slot | newStart | push | absorbed | newDur | newEnd | note |
|------|------|----------|------|----------|--------|--------|------|
|  1   |  B   |  11:00   |  60  |   10     |  50    | 11:50  | displaced +50 |
|  2   |  C   |  11:50   |  50  |   10     |  50    | 12:40  | displaced +40 |
|  3   |  D   |  12:40   |  40  |    0     |  30    | 13:10  | rigid, pushed +40 |

Result: `OVERFLOW`, `overflowMinutes = 40` (D's original end 12:30 → 13:10).
A rigid slot was pushed by a non-rigid one, and would in turn push any rigid slot
behind it. All three of B, C, D are `displaced` and notified.

---

## 5. The "unresolvable" condition, made precise

The primer says the engine may emit an **"unresolvable"** flag. v1 splits this
into two distinct, separately-handled conditions:

### 5.1 `OVERFLOW` (soft, exists in v1)

Delay leaked past the queue's flexibility; the tail runs late. **Always
representable** — the schedule is valid (no overlaps), just later than hoped.
Handling: notify displaced users; let them cancel/rebook. No engine failure.

### 5.2 `INFEASIBLE` (hard, **deferred to post-MVP**)

Arises *only* if we introduce **wall-clock anchors** (§2.3) — a slot that *cannot*
move in absolute time. Then a delay that would push an anchored slot has *no legal
resolution*: the upstream slot can neither shrink enough nor push the anchor. That
is a genuine conflict requiring a human decision (bump someone, split the booking,
etc.).

v1 deliberately has **no** anchors, so the engine **never** returns `INFEASIBLE`.
The result type should still model it (so adding anchors later is additive, not a
breaking change), but the v1 implementation can treat it as unreachable.

> **Decision flagged:** confirm v1 ships with soft-`OVERFLOW` only and no hard
> anchors. This keeps the engine total (always produces a valid schedule).

---

## 6. Algorithm 2 — Pull-forward on cancel / early finish

**Trigger:**
- **Early finish:** an `ACTIVE` slot completes before its scheduled end. The freed
  instant `freeFrom` = its actual end.
- **Cancellation:** a `SCHEDULED` (or `ACTIVE`) slot is removed. `freeFrom` = the
  end of whatever now precedes the gap (the cancelled slot's predecessor's end, or
  `now` if it was active).

Slots behind the gap may move **earlier**, keeping full duration, each by at most
its own `window`.

```
function pullForward(slots, freeFrom, firstAffectedIndex):
    result = copy(slots)
    prevEnd = freeFrom
    for i from firstAffectedIndex to last:
        if slots[i].status != SCHEDULED:
            continue
        earliest = max(prevEnd, slots[i].start - slots[i].window)
        if earliest >= slots[i].start:
            break                      # this slot can't (or needn't) move earlier;
                                        # if it's rigid it BLOCKS everything behind it
        result[i].start = earliest      # duration unchanged on pull-forward
        prevEnd = earliest + slots[i].duration
    return result
```

### 6.1 Worked example — pull-forward chain, blocked by a rigid slot

```
A  09:00  dur 60  ACTIVE  → finishes early at 09:40    (freeFrom = 09:40)
B  10:00  dur 60  win 20  SCHEDULED
C  11:00  dur 30  win 0   SCHEDULED (rigid)
D  11:30  dur 30  win 15  SCHEDULED
```
| step | slot | earliest                         | moves? | newStart | newEnd |
|------|------|----------------------------------|--------|----------|--------|
|  1   |  B   | max(09:40, 10:00−20=09:40)=09:40 |  yes   | 09:40    | 10:40  |
|  2   |  C   | max(10:40, 11:00−0 =11:00)=11:00 |  no    |   —      |   —    |
|  →   |      | `earliest ≥ start` → **break**; C is rigid and blocks D | | | |

Result: B pulled up 20 min; C and D unchanged. A rigid slot in the queue is a
wall the pull-forward can't move past — by design.

### 6.2 Worked example — cancellation, chain pulls forward within windows

```
(A completed 10:00)   B is CANCELLED (was 10:00–11:00)   →  freeFrom = 10:00
C  11:00  dur 60  win 30  SCHEDULED
D  12:00  dur 30  win 10  SCHEDULED
```
| step | slot | earliest                         | newStart | newEnd |
|------|------|----------------------------------|----------|--------|
|  1   |  C   | max(10:00, 11:00−30=10:30)=10:30 | 10:30    | 11:30  |
|  2   |  D   | max(11:30, 12:00−10=11:50)=11:50 | 11:50    | 12:20  |

Result: C and D each pulled forward as far as their windows allow. Note a residual
idle gap can remain (e.g. 10:00–10:30) when windows are too small to fully close
it — that's correct, not a bug.

### 6.3 Auto-apply vs. propose  ⚠️ REVIEW THIS

> **Decision flagged:** does pull-forward *automatically* move users earlier, or
> merely *propose* the earlier time? Being silently told "come 25 minutes earlier"
> can be worse than a gap. The defensible default: **auto-apply, because the move
> is bounded by `window`, which is exactly the flexibility the user consented to.**
> Anyone who wants zero earliness sets `window`'s pull-forward behavior off (or
> books rigid). Confirm this, or we split `window` into separate forward/back
> budgets (more model, more UI).

---

## 7. Booking / inserting a slot (lighter, v1)

Creating a slot is placement, not re-flow, but it can *seed* a cascade:
- If the requested `[start, start+duration)` lands in free time → insert, done.
- If it overlaps existing scheduled slots and the new slot is **flexible** →
  reject with the nearest free suggestion (keep v1 simple; no auto-fit).
- If the new slot is **rigid/anchored-intent** and the head explicitly forces it →
  treat as a delay event seeded at the new slot's end and `cascadeDelay` the
  overlapped slots forward. (This is the "fixed pushes fixed" booking path.)

> **Decision flagged:** v1 booking policy = *reject flexible overlaps with a
> suggestion; only forced/privileged inserts cascade.* Confirm, or we build
> best-fit auto-placement (post-MVP).

---

## 8. Edge cases the implementation must cover

| Case | Expected behavior |
|------|-------------------|
| Empty queue / single slot | No-op; return input unchanged. |
| Delta ≤ 0 on a "delay" event | No-op (or reject); never a cascade. |
| Gap larger than delta | `max(start, prevEnd)` settles it; cascade breaks early. |
| One slot absorbs everything | Cascade stops at that slot; tail untouched. |
| Every slot rigid (`window 0`) | Any overrun overflows by the full delta; whole tail shifts. |
| Duration floor hit | Slot shrinks only to `MIN_DURATION`; remainder propagates as delta. |
| Rigid slot blocks pull-forward | Pull-forward stops at it; downstream gap remains. |
| Window > remaining duration | Impossible if invariant #4 enforced at creation; assert it. |
| Cancel the active slot | Treat as early finish with `freeFrom = now`. |
| Overrun detected mid-session | See §9 — projected vs. settled. |
| Concurrent events | Serialized by the caller's transaction (§9). |

---

## 9. Implementation contract (how Phase 6 / Phase 7 must use this)

- **Pure functions, no I/O.** The engine takes slices/structs in and returns
  them out. No DB, no HTTP, no clock reads, no logging of business state inside
  the core. The caller injects `now`, the seed end, and `MIN_DURATION`.
- **Convert at the edges.** proto ⇄ domain and DB-row ⇄ domain conversions live in
  the handler/repository, never inside the engine.
- **One transaction per event.** A cascade touches many rows; the caller loads the
  equipment's scheduled slots `FOR UPDATE`, runs the engine, and persists the whole
  result atomically, so no observer ever sees a half-shifted queue (PLAN Phase 7).
- **Re-cascade baseline (⚠️ REVIEW):** each event is resolved against the slots'
  *current* persisted state; `window` is treated as per-event flexibility and is
  **not "used up"** across successive events. This is the simplest correct model.
  Risk: repeated overruns could compress a slot across multiple events down to the
  `MIN_DURATION` floor. The floor bounds the damage; a fuller fix (track each
  slot's *original* schedule and cap cumulative drift) is a flagged refinement.
- **Overrun detection (⚠️ REVIEW):** decide whether the cascade runs on a *live
  projection* (the active slot is past its scheduled end → recompute continuously
  so downstream ETAs update in real time) or only *settles on clock-out*. Live
  projection gives better notifications; settle-on-clock-out is simpler. Recommend
  starting with settle-on-clock-out + a single "projected overrun" recompute when
  the active slot crosses its scheduled end, and revisit.

---

## 10. Test matrix (drives the Phase 6 table-driven `testify` suite)

Each row is one table-test case; expected output is the full resolved queue +
result classification.

**Cascade (delay):**
1. Empty queue — no-op.
2. Single active slot, nothing behind — no-op.
3. Delta fully absorbed by a leading gap — cascade breaks immediately.
4. Full absorption by the first downstream slot — tail untouched, `ABSORBED`.
5. Partial absorption, settles mid-queue — `ABSORBED` (the §4.3 example).
6. Overflow past the tail — `OVERFLOW` with exact `overflowMinutes` (the §4.4 ex).
7. Rigid slot pushed by a flexible one — verify it shifts wholesale.
8. Chain of rigid slots — each pushes the next by the full residual.
9. Duration-floor hit — slot stops at `MIN_DURATION`, remainder propagates.
10. All-rigid queue — entire tail shifts by the full delta.
11. Delta ≤ 0 — rejected/no-op.

**Pull-forward:**
12. Single slot pulled forward within window.
13. Chain pulls forward (the §6.2 example).
14. Blocked by a rigid slot (the §6.1 example).
15. Window too small to close the gap — residual idle gap remains.
16. Cancel the active slot — handled as early finish.
17. Cancellation of a middle slot with `COMPLETE`/`CANCELLED` neighbors skipped.

**Invariants (assert on every case above):** no overlap, order preserved,
duration ≥ floor, history untouched, determinism (run twice, compare).

---

## 11. Open decisions for review (consolidated)

These are flagged inline above; collected here so you can sign off in one pass:

1. **§2.1 / §2.2** — `window` = *compression on delay* + *earliness on
   pull-forward*. Confirm, or split into two budgets.
2. **§2.3 / §5.2** — `window == 0` = *rigid*, not *wall-clock anchored*; hard
   anchors (and the resulting `INFEASIBLE` state) are post-MVP. Confirm.
3. **§6.3** — pull-forward *auto-applies* within `window` rather than merely
   proposing. Confirm.
4. **§7** — v1 booking *rejects* flexible overlaps (with a suggestion); only
   forced/privileged inserts cascade. Confirm.
5. **§9** — re-cascade resolves against *current* state; `window` not consumed
   across events; `MIN_DURATION` floor is the only guard against repeated
   compression. Confirm or schedule the cumulative-drift refinement.
6. **§9** — overrun handling = *settle on clock-out* + one projected recompute at
   the scheduled-end crossing. Confirm.
7. **§6** — value of `MIN_DURATION` (e.g. 5 min?). Set it.
