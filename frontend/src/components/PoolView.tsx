import { SlotStatus } from "../protogen/qlab/v1/types_pb";
import {
  SlotActions,
  type SlotHandlers,
  type SlotRow,
  StatusPill,
  bookButtonClass,
} from "./slot-ui";

// PoolView is the product's single view for one pool: a "running now" grid (one cell
// per resource, since a slot is pinned to a resource only once it clocks in) on top,
// then the upcoming queue as a vertical timeline with "now" at the top flowing down.
// Slots are unassigned until they start, so only the grid shows a resource. The
// timeline renders both booked slots and the unfilled gaps between them, each
// proportional to its duration — so the axis reads as real (compressed) elapsed time.

interface PoolViewProps extends SlotHandlers {
  poolName: string;
  resourceCount: number;
  rows: SlotRow[];
  pending?: boolean;
  onBook: () => void;
}

function formatTime(date?: Date): string {
  return date ? date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : "—";
}

function endOf(row: SlotRow): Date | null {
  return row.start ? new Date(row.start.getTime() + row.durationMinutes * 60000) : null;
}

// heightFor maps a duration to a card height: proportional but compressed — a floor so
// short slots stay readable, a ceiling so a very long booking doesn't dominate.
function heightFor(durationMinutes: number): number {
  return Math.max(64, Math.min(320, Math.round(durationMinutes * 1.6)));
}

// Gaps get a thinner floor than slots — they carry little content, so a short opening
// should read as a thin sliver.
function gapHeightFor(minutes: number): number {
  return Math.max(36, Math.min(160, Math.round(minutes * 1.6)));
}

function reachHeightFor(minutes: number): number {
  return Math.max(32, Math.min(200, Math.round(minutes * 1.6)));
}

// forwardReachMinutes is how far the viewer's own slot could be pulled forward — the
// span between its earliest reachable start and its current projected start. Zero
// (no outline) unless it's the viewer's slot with real forward room.
function forwardReachMinutes(row: SlotRow): number {
  if (!row.isYou || !row.start || !row.earliestStart || row.earliestStart >= row.start) {
    return 0;
  }
  return Math.round((row.start.getTime() - row.earliestStart.getTime()) / 60000);
}

// ForwardReach is the dashed outline above the viewer's card: "you're here, but you
// could move up to here if capacity frees (someone cancels / finishes early)" — the
// lookahead window made visible. Connects to the card below (flat bottom).
function ForwardReach({ minutes, earliest }: { minutes: number; earliest: Date }) {
  return (
    <div
      className="flex items-start justify-center rounded-t-lg border border-teal/50 border-b-0 border-dashed bg-teal/5"
      style={{ minHeight: reachHeightFor(minutes) }}
    >
      <span className="pt-1.5 text-teal text-xs">↑ as early as {formatTime(earliest)}</span>
    </div>
  );
}

// RunningGrid shows what's on each resource right now: the active slot pinned to it, or
// "Free". Resource names are generic for now (interchangeable; named resources later).
function RunningGrid({
  resourceCount,
  active,
  pending,
  handlers,
}: {
  resourceCount: number;
  active: SlotRow[];
  pending?: boolean;
  handlers: SlotHandlers;
}) {
  const cells = Array.from({ length: resourceCount }, (_, i) => {
    const name = `Hood ${i + 1}`;
    return { name, slot: active.find((s) => s.resourceLabel === name) ?? null };
  });
  return (
    <div className="grid grid-cols-2 gap-2">
      {cells.map(({ name, slot }) => (
        <div
          key={name}
          className={`rounded-lg border bg-base p-3 ${slot?.overrun ? "border-danger/50" : "border-edge"}`}
        >
          <div className="flex items-center justify-between gap-2">
            <span className="text-muted text-xs">{name}</span>
            {slot && <StatusPill row={slot} />}
          </div>
          {slot ? (
            <>
              <div className="mt-1.5 text-sm">
                {slot.userLabel}
                {slot.isYou && <span className="text-muted"> · you</span>}
              </div>
              <div className="text-muted text-xs tabular-nums">
                {slot.overrun ? "over since " : "ends "}
                {formatTime(endOf(slot) ?? undefined)}
              </div>
              <div className="mt-2 flex justify-end">
                <SlotActions row={slot} pending={pending} handlers={handlers} />
              </div>
            </>
          ) : (
            <div className="mt-4 text-center text-muted text-sm">Free</div>
          )}
        </div>
      ))}
    </div>
  );
}

// A booked slot card: a full-height left accent bar (amber = flexible / has lookahead,
// neutral = fixed), status + who, the duration/flex line, and contextual actions
// pinned to the bottom so a tall card doesn't read as empty.
function SlotCard({
  row,
  pending,
  handlers,
  flatTop,
}: {
  row: SlotRow;
  pending?: boolean;
  handlers: SlotHandlers;
  flatTop?: boolean;
}) {
  return (
    <div
      className={`flex flex-1 overflow-hidden border border-edge bg-base ${flatTop ? "rounded-b-lg" : "rounded-lg"}`}
      style={{ minHeight: heightFor(row.durationMinutes) }}
    >
      <span className={`w-1 shrink-0 ${row.lookaheadMinutes > 0 ? "bg-amber" : "bg-edge"}`} />
      <div className="flex flex-1 flex-col p-3">
        <div className="flex items-center gap-2">
          <StatusPill row={row} />
          <span className="text-sm">
            {row.userLabel}
            {row.isYou && <span className="text-muted"> · you</span>}
          </span>
        </div>
        <div className="mt-1.5 text-muted text-xs tabular-nums">
          {row.durationMinutes}m
          {row.lookaheadMinutes > 0 && (
            <span className="text-amber"> · flex {row.lookaheadMinutes}m</span>
          )}
        </div>
        <div className="mt-auto flex justify-end pt-2">
          <SlotActions row={row} pending={pending} handlers={handlers} />
        </div>
      </div>
    </div>
  );
}

// A gap card: unfilled, bookable time, shown with faint diagonal stripes.
function GapCard({ minutes }: { minutes: number }) {
  return (
    <div
      className="stripe-open flex flex-1 items-center justify-center rounded-lg border border-edge border-dashed"
      style={{ minHeight: gapHeightFor(minutes) }}
    >
      <span className="text-muted text-xs">{minutes}m open</span>
    </div>
  );
}

type Segment =
  | { kind: "slot"; key: string; start?: Date; row: SlotRow }
  | { kind: "gap"; key: string; start: Date; minutes: number };

// buildSegments walks the upcoming slots in time order, emitting a gap segment wherever
// there's idle time between one slot's end and the next slot's start.
function buildSegments(upcoming: SlotRow[]): Segment[] {
  const sorted = [...upcoming].sort(
    (a, b) => (a.start?.getTime() ?? 0) - (b.start?.getTime() ?? 0),
  );
  const segments: Segment[] = [];
  let cursor: Date | null = null;
  for (const row of sorted) {
    if (cursor && row.start && row.start.getTime() > cursor.getTime()) {
      const minutes = Math.round((row.start.getTime() - cursor.getTime()) / 60000);
      if (minutes >= 5) {
        segments.push({ kind: "gap", key: `gap-${row.slotId}`, start: cursor, minutes });
      }
    }
    segments.push({ kind: "slot", key: row.slotId, start: row.start, row });
    cursor = endOf(row) ?? cursor;
  }
  return segments;
}

function UpcomingTimeline({
  rows,
  pending,
  handlers,
}: {
  rows: SlotRow[];
  pending?: boolean;
  handlers: SlotHandlers;
}) {
  const segments = buildSegments(rows);
  return (
    <div>
      <div className="flex items-center gap-3 pb-1">
        <span className="w-12 shrink-0 text-right font-medium text-teal text-xs">now</span>
        <span className="h-px flex-1 bg-teal/40" />
      </div>
      {segments.length === 0 ? (
        <p className="py-6 text-center text-muted text-sm">Nothing queued.</p>
      ) : (
        <ol className="flex flex-col">
          {segments.map((seg) => (
            <li key={seg.key} className="flex gap-3 py-1.5">
              <time className="w-12 shrink-0 pt-2.5 text-right text-muted text-xs tabular-nums">
                {formatTime(seg.start)}
              </time>
              {seg.kind === "slot" ? (
                (() => {
                  const reach = forwardReachMinutes(seg.row);
                  return reach > 0 && seg.row.earliestStart ? (
                    <div className="flex flex-1 flex-col">
                      <ForwardReach minutes={reach} earliest={seg.row.earliestStart} />
                      <SlotCard row={seg.row} pending={pending} handlers={handlers} flatTop />
                    </div>
                  ) : (
                    <SlotCard row={seg.row} pending={pending} handlers={handlers} />
                  );
                })()
              ) : (
                <GapCard minutes={seg.minutes} />
              )}
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

export function PoolView(props: PoolViewProps) {
  const { rows, pending } = props;
  const handlers: SlotHandlers = props;
  const active = rows.filter((r) => r.status === SlotStatus.ACTIVE);
  const upcoming = rows.filter((r) => r.status === SlotStatus.SCHEDULED);

  return (
    <section className="overflow-hidden rounded-xl border border-edge bg-surface text-fg">
      <header className="flex items-center justify-between border-edge border-b px-4 py-3">
        <h2 className="text-sm">
          <span className="font-semibold text-teal">{props.poolName}</span>
          <span className="text-muted"> · {props.resourceCount} resources</span>
        </h2>
        <button type="button" disabled={pending} className={bookButtonClass} onClick={props.onBook}>
          + Book slot
        </button>
      </header>
      <div className="flex flex-col gap-5 p-4">
        <RunningGrid
          resourceCount={props.resourceCount}
          active={active}
          pending={pending}
          handlers={handlers}
        />
        <UpcomingTimeline rows={upcoming} pending={pending} handlers={handlers} />
      </div>
    </section>
  );
}
