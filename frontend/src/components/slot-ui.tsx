import { SlotStatus } from "../protogen/qlab/v1/types_pb";

// Shared slot UI: the view-model, the status pill, and the contextual action buttons.
// Both the (now timeline) product view and any future dense view render off these, so
// "what state is this" and "what can the viewer do" live in exactly one place.

export interface SlotRow {
  slotId: string;
  priority: number;
  userLabel: string;
  isYou: boolean;
  status: SlotStatus;
  // start is the placed start (actual) or, before placement, the desired start.
  start?: Date;
  durationMinutes: number;
  lookaheadMinutes: number;
  // overrun: ACTIVE and past its scheduled end. reclaimable: SCHEDULED and the
  // clock-in grace lapsed. youAreNext: the viewer is next-in-line for this resource.
  overrun: boolean;
  reclaimable: boolean;
  youAreNext: boolean;
  // resourceId: which resource an ACTIVE slot is pinned to (matched to a grid cell by
  // id). Empty for SCHEDULED slots — they aren't assigned a resource until they start.
  resourceId?: string;
  // earliestStart: the earliest this slot could be pulled forward to — its
  // lookahead floor (desired_start - lookahead), clamped to now and to reachable
  // capacity. Set only for the viewer's own scheduled slots; drives the "you could
  // move up to here" reach outline. Absent ⇒ no forward room to show.
  earliestStart?: Date;
}

// A cell in the running-now grid: a stable resource id to match ACTIVE slots against,
// plus its display label. Built once in the container so the label scheme lives in one
// place; the view matches by id, never by re-deriving the label.
export interface ResourceCell {
  id: string;
  label: string;
}

// The per-slot action callbacks a container wires to the mutating RPCs.
export interface SlotHandlers {
  onClockIn: (slotId: string) => void;
  onClockOut: (slotId: string) => void;
  onCancel: (slotId: string) => void;
  onPoke: (slotId: string) => void;
  onForceClockOut: (slotId: string) => void;
  onForceNoShow: (slotId: string) => void;
}

type Tone = "teal" | "amber" | "danger" | "muted";

// statusBadge maps a row's effective state to a label + tone. Overrun and reclaimable
// override the raw status because they're what the viewer acts on.
function statusBadge(row: SlotRow): { label: string; tone: Tone } {
  if (row.overrun) {
    return { label: "Overrun", tone: "danger" };
  }
  if (row.reclaimable) {
    return { label: "No-show?", tone: "amber" };
  }
  switch (row.status) {
    case SlotStatus.ACTIVE:
      return { label: "Active", tone: "teal" };
    case SlotStatus.SCHEDULED:
      return { label: "Scheduled", tone: "muted" };
    case SlotStatus.COMPLETE:
      return { label: "Done", tone: "muted" };
    case SlotStatus.CANCELLED:
      return { label: "Cancelled", tone: "muted" };
    case SlotStatus.NO_SHOW:
      return { label: "No-show", tone: "muted" };
    default:
      return { label: "—", tone: "muted" };
  }
}

const pillTone: Record<Tone, string> = {
  teal: "bg-teal/15 text-teal",
  amber: "bg-amber/15 text-amber",
  danger: "bg-danger/15 text-danger",
  muted: "bg-edge text-muted",
};

export function StatusPill({ row }: { row: SlotRow }) {
  const { label, tone } = statusBadge(row);
  return (
    <span className={`inline-block rounded-full px-2 py-0.5 font-medium text-xs ${pillTone[tone]}`}>
      {label}
    </span>
  );
}

const btnBase = "rounded-md px-2.5 py-1 font-medium text-xs disabled:opacity-40";
const btnPrimary = `${btnBase} bg-teal text-base hover:opacity-90`;
const btnDanger = `${btnBase} bg-danger text-base hover:opacity-90`;
const btnSubtle = `${btnBase} bg-edge text-fg hover:opacity-90`;

// SlotActions renders only the controls the viewer can use on this slot right now —
// own ACTIVE → clock out / cancel; own SCHEDULED → clock in / cancel; someone ahead
// overrunning and you're next → poke / boot; ahead no-show grace lapsed and you're next
// → reclaim. Authorization is enforced server-side; this just hides actions that would
// obviously fail. Returns null when there's nothing to do.
export function SlotActions({
  row,
  pending,
  handlers,
}: {
  row: SlotRow;
  pending?: boolean;
  handlers: SlotHandlers;
}) {
  if (row.isYou && row.status === SlotStatus.ACTIVE) {
    // Cancel an in-progress slot too (abandon vs finish) — CancelSlot settles an ACTIVE
    // slot server-side, freeing the resource as a cancellation rather than a completion.
    return (
      <div className="flex gap-1.5">
        <button
          type="button"
          disabled={pending}
          className={btnPrimary}
          onClick={() => handlers.onClockOut(row.slotId)}
        >
          Clock out
        </button>
        <button
          type="button"
          disabled={pending}
          className={btnSubtle}
          onClick={() => handlers.onCancel(row.slotId)}
        >
          Cancel
        </button>
      </div>
    );
  }
  if (row.isYou && row.status === SlotStatus.SCHEDULED) {
    return (
      <div className="flex gap-1.5">
        <button
          type="button"
          disabled={pending}
          className={btnPrimary}
          onClick={() => handlers.onClockIn(row.slotId)}
        >
          Clock in
        </button>
        <button
          type="button"
          disabled={pending}
          className={btnSubtle}
          onClick={() => handlers.onCancel(row.slotId)}
        >
          Cancel
        </button>
      </div>
    );
  }
  if (!row.isYou && row.youAreNext && row.overrun) {
    return (
      <div className="flex gap-1.5">
        <button
          type="button"
          disabled={pending}
          className={btnSubtle}
          onClick={() => handlers.onPoke(row.slotId)}
        >
          Poke
        </button>
        <button
          type="button"
          disabled={pending}
          className={btnDanger}
          onClick={() => handlers.onForceClockOut(row.slotId)}
        >
          Boot
        </button>
      </div>
    );
  }
  if (!row.isYou && row.youAreNext && row.reclaimable) {
    return (
      <button
        type="button"
        disabled={pending}
        className={btnDanger}
        onClick={() => handlers.onForceNoShow(row.slotId)}
      >
        Reclaim
      </button>
    );
  }
  return null;
}

export const bookButtonClass = btnPrimary;
