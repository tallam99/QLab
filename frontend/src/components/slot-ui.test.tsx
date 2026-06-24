import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SlotStatus } from "../protogen/qlab/v1/types_pb";
import { SlotActions, type SlotHandlers, type SlotRow } from "./slot-ui";

// SlotActions is the one place "what can the viewer do to this slot" lives, so its
// contextual rendering is worth pinning: each state must show exactly its actions and
// nothing else (authorization is still enforced server-side; this just hides no-ops).
const noopHandlers: SlotHandlers = {
  onClockIn: () => {},
  onClockOut: () => {},
  onCancel: () => {},
  onPoke: () => {},
  onForceClockOut: () => {},
  onForceNoShow: () => {},
};

function row(partial: Partial<SlotRow>): SlotRow {
  return {
    slotId: "s",
    priority: 1,
    userLabel: "U",
    isYou: false,
    status: SlotStatus.SCHEDULED,
    durationMinutes: 30,
    lookaheadMinutes: 0,
    overrun: false,
    reclaimable: false,
    youAreNext: false,
    ...partial,
  };
}

// label set per state; absent labels must NOT render.
const ALL = ["Clock in", "Clock out", "Cancel", "Poke", "Boot", "Reclaim"];

const cases: { name: string; row: SlotRow; shows: string[] }[] = [
  {
    name: "own ACTIVE → clock out + cancel",
    row: row({ isYou: true, status: SlotStatus.ACTIVE }),
    shows: ["Clock out", "Cancel"],
  },
  {
    name: "own SCHEDULED → clock in + cancel",
    row: row({ isYou: true, status: SlotStatus.SCHEDULED }),
    shows: ["Clock in", "Cancel"],
  },
  {
    name: "other + next + overrun → poke + boot",
    row: row({ isYou: false, status: SlotStatus.ACTIVE, overrun: true, youAreNext: true }),
    shows: ["Poke", "Boot"],
  },
  {
    name: "other + next + reclaimable → reclaim",
    row: row({ isYou: false, reclaimable: true, youAreNext: true }),
    shows: ["Reclaim"],
  },
  {
    name: "other, not next → nothing",
    row: row({ isYou: false, overrun: true, youAreNext: false }),
    shows: [],
  },
];

describe("SlotActions", () => {
  it.each(cases)("$name", ({ row: r, shows }) => {
    render(<SlotActions row={r} handlers={noopHandlers} />);
    for (const label of ALL) {
      const button = screen.queryByRole("button", { name: label });
      if (shows.includes(label)) {
        expect(button).not.toBeNull();
      } else {
        expect(button).toBeNull();
      }
    }
  });
});
