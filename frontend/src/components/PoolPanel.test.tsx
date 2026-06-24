import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import type { RescheduleResult } from "../protogen/qlab/v1/types_pb";
import { SlotStatus } from "../protogen/qlab/v1/types_pb";
import { resultToRows } from "./PoolPanel";

// resultToRows is the wiring's testable core: the engine result (slots + positions)
// mapped to view rows, with overrun / earliestStart / resource label derived. Build
// minimal proto-shaped objects (the function only reads fields).
const ts = (iso: string) => timestampFromDate(new Date(iso));
const now = new Date("2026-06-24T11:00:00");

function makeResult(): RescheduleResult {
  return {
    resourcePoolId: "pool-1",
    slots: [
      // ACTIVE, overrunning: started 10:00 for 30m (ended 10:30, before now 11:00).
      {
        id: "a",
        userId: "u1",
        status: SlotStatus.ACTIVE,
        actualStart: ts("2026-06-24T10:00:00"),
        durationMinutes: 30,
        lookaheadMinutes: 0,
        slotPriority: 1,
        assignedResourceId: "r1",
      },
      // The viewer's SCHEDULED slot at 12:00 with 60m lookahead → could reach 11:00.
      {
        id: "b",
        userId: "me",
        status: SlotStatus.SCHEDULED,
        actualStart: ts("2026-06-24T12:00:00"),
        desiredStart: ts("2026-06-24T12:00:00"),
        durationMinutes: 30,
        lookaheadMinutes: 60,
        slotPriority: 2,
        assignedResourceId: "",
      },
    ],
    positions: [{ slotId: "b", reclaimable: false, recommitted: false, assignedResourceId: "" }],
  } as unknown as RescheduleResult;
}

describe("resultToRows", () => {
  it("derives overrun, isYou, resource label, and the forward-reach floor", () => {
    const rows = resultToRows(makeResult(), {
      currentUserId: "me",
      now,
      memberLabel: new Map([
        ["u1", "Ann"],
        ["me", "You"],
      ]),
      resourceLabel: new Map([["r1", "Hood 1"]]),
    });

    expect(rows).toHaveLength(2);
    const [active, mine] = rows;

    // Active overrunning slot, on its resource, not the viewer's.
    expect(active.overrun).toBe(true);
    expect(active.isYou).toBe(false);
    expect(active.userLabel).toBe("Ann");
    expect(active.resourceLabel).toBe("Hood 1");

    // The viewer's scheduled slot: no resource (unassigned), reach floor clamped to now.
    expect(mine.isYou).toBe(true);
    expect(mine.resourceLabel).toBeUndefined();
    expect(mine.earliestStart?.getTime()).toBe(now.getTime());
  });

  it("returns nothing for an undefined result", () => {
    expect(
      resultToRows(undefined, {
        currentUserId: "me",
        now,
        memberLabel: new Map(),
        resourceLabel: new Map(),
      }),
    ).toEqual([]);
  });
});
