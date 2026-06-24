import { PoolView } from "./components/PoolView";
import { ThemeToggle } from "./components/ThemeToggle";
import type { SlotRow } from "./components/slot-ui";
import { SlotStatus } from "./protogen/qlab/v1/types_pb";

// Preview is the dev-only component gallery (Stage B of the design loop): product
// components rendered with mock data in every state, so the visual can be reviewed and
// tweaked without the API or real auth. Reached at /preview in dev only (main.tsx).

const at = (hour: number, minute: number) => new Date(2026, 5, 24, hour, minute);

// One scenario (viewer = "Ann"). Active slots are pinned to a hood; scheduled slots are
// unassigned (no resource) and vary in duration to show the proportional heights.
const rows: SlotRow[] = [
  {
    slotId: "1",
    priority: 1,
    userLabel: "Ann",
    isYou: true,
    status: SlotStatus.ACTIVE,
    start: at(10, 0),
    durationMinutes: 30,
    lookaheadMinutes: 0,
    overrun: false,
    reclaimable: false,
    youAreNext: false,
    resourceLabel: "Hood 1",
  },
  {
    slotId: "2",
    priority: 2,
    userLabel: "Bo",
    isYou: false,
    status: SlotStatus.ACTIVE,
    start: at(10, 15),
    durationMinutes: 45,
    lookaheadMinutes: 0,
    overrun: true,
    reclaimable: false,
    youAreNext: true,
    resourceLabel: "Hood 2",
  },
  {
    slotId: "3",
    priority: 3,
    userLabel: "Cal",
    isYou: false,
    status: SlotStatus.SCHEDULED,
    start: at(11, 0),
    durationMinutes: 30,
    lookaheadMinutes: 0,
    overrun: false,
    reclaimable: false,
    youAreNext: false,
  },
  {
    slotId: "4",
    priority: 4,
    userLabel: "Mia",
    isYou: true,
    status: SlotStatus.SCHEDULED,
    start: at(12, 0),
    durationMinutes: 30,
    lookaheadMinutes: 60,
    overrun: false,
    reclaimable: false,
    youAreNext: false,
    earliestStart: at(11, 0),
  },
  {
    slotId: "5",
    priority: 5,
    userLabel: "Dee",
    isYou: false,
    status: SlotStatus.SCHEDULED,
    start: at(13, 15),
    durationMinutes: 120,
    lookaheadMinutes: 30,
    overrun: false,
    reclaimable: false,
    youAreNext: false,
  },
];

export function Preview() {
  const noop = () => {};
  const handlers = {
    onBook: noop,
    onClockIn: noop,
    onClockOut: noop,
    onCancel: noop,
    onPoke: noop,
    onForceClockOut: noop,
    onForceNoShow: noop,
  };
  return (
    <div className="min-h-screen bg-base text-fg">
      <div className="mx-auto grid max-w-md gap-8 p-6">
        <header className="flex items-center justify-between">
          <h1 className="text-lg">
            Component preview <span className="text-muted">· PoolView</span>
          </h1>
          <ThemeToggle />
        </header>
        <div>
          <p className="mb-2 text-muted text-xs uppercase tracking-wide">
            Populated (viewer = Mia)
          </p>
          <PoolView poolName="Vent Hoods" resourceCount={2} rows={rows} {...handlers} />
        </div>
        <div>
          <p className="mb-2 text-muted text-xs uppercase tracking-wide">Empty</p>
          <PoolView poolName="Vent Hoods" resourceCount={2} rows={[]} {...handlers} />
        </div>
      </div>
    </div>
  );
}
