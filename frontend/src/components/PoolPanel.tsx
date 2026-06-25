import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { timestampDate, timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { useEffect, useMemo, useState } from "react";
import { usePoolScheduleStream } from "../api/usePoolScheduleStream";
import { QlabService } from "../protogen/qlab/v1/service_pb";
import type { RescheduleResult } from "../protogen/qlab/v1/types_pb";
import { SlotStatus } from "../protogen/qlab/v1/types_pb";
import { useWorkspace } from "../workspace/WorkspaceProvider";
import { PoolView } from "./PoolView";
import type { ResourceCell, SlotRow } from "./slot-ui";

function toDate(ts?: Timestamp): Date | undefined {
  return ts ? timestampDate(ts) : undefined;
}

function errMessage(error: unknown): string {
  return error instanceof ConnectError ? `${Code[error.code]}: ${error.rawMessage}` : String(error);
}

interface MapContext {
  currentUserId: string;
  now: Date;
  memberLabel: Map<string, string>;
}

// resultToRows turns a GetSchedule RescheduleResult into the view's SlotRow[]. The
// engine supplies placement + reclaimable (via positions); overrun, earliestStart, and
// next-in-line are derived here from data already on the slot.
export function resultToRows(result: RescheduleResult | undefined, ctx: MapContext): SlotRow[] {
  if (!result) {
    return [];
  }
  const posBySlot = new Map(result.positions.map((p) => [p.slotId, p]));
  const reclaimable = (slotId: string) => posBySlot.get(slotId)?.reclaimable ?? false;

  // youAreNext gates poke/boot/reclaim. The next-in-line is whoever owns the front-most
  // SCHEDULED slot that is itself runnable — i.e. excluding a lapsed no-show, since that
  // slot is the one being reclaimed, not the one reclaiming it. (Without that exclusion
  // the no-show owner looks "next" and Reclaim never shows for the actual next user.)
  // An approximation; the backend enforces the real check.
  const nextInLine = result.slots
    .filter((s) => s.status === SlotStatus.SCHEDULED && !reclaimable(s.id))
    .reduce<{ userId: string; slotPriority: number } | null>(
      (min, s) => (!min || s.slotPriority < min.slotPriority ? s : min),
      null,
    );
  const youAreNextInLine = nextInLine ? nextInLine.userId === ctx.currentUserId : false;

  return result.slots.map((s) => {
    const start = toDate(s.actualStart) ?? toDate(s.desiredStart);
    const isYou = s.userId === ctx.currentUserId;
    const overrun =
      s.status === SlotStatus.ACTIVE &&
      !!start &&
      start.getTime() + s.durationMinutes * 60000 < ctx.now.getTime();
    const slotReclaimable = reclaimable(s.id);

    let earliestStart: Date | undefined;
    if (isYou && s.status === SlotStatus.SCHEDULED && s.lookaheadMinutes > 0) {
      const desired = toDate(s.desiredStart);
      if (desired && start) {
        const floor = Math.max(ctx.now.getTime(), desired.getTime() - s.lookaheadMinutes * 60000);
        if (floor < start.getTime()) {
          earliestStart = new Date(floor);
        }
      }
    }

    return {
      slotId: s.id,
      priority: s.slotPriority,
      userLabel: ctx.memberLabel.get(s.userId) ?? s.userId.slice(0, 8),
      isYou,
      status: s.status,
      start,
      durationMinutes: s.durationMinutes,
      lookaheadMinutes: s.lookaheadMinutes,
      overrun,
      reclaimable: slotReclaimable,
      youAreNext: !isYou && (overrun || slotReclaimable) ? youAreNextInLine : false,
      resourceId: s.status === SlotStatus.ACTIVE ? s.assignedResourceId : undefined,
      earliestStart,
    };
  });
}

// PoolPanel is the container behind the product view: it loads the pool's schedule via
// the read-only GetSchedule, maps it to rows, and wires the seven mutating RPCs. The
// acting user's mutation refetches for immediate feedback; the live SSE stream pushes
// every change (including other users') into the same query cache, so the queue stays
// current without a refresh.
export function PoolPanel() {
  const { poolId, canQuery, actingUserId, workspace } = useWorkspace();
  const { data, error, isLoading, refetch } = useQuery(
    QlabService.method.getSchedule,
    { resourcePoolId: poolId ?? "" },
    { enabled: canQuery },
  );

  // Live updates: subscribe to the pool's schedule stream and let it write the
  // GetSchedule cache (above) directly — a clock-out in another browser updates here
  // with no refetch. Gated to a selected pool inside the hook.
  usePoolScheduleStream(canQuery ? poolId : null);

  const createSlot = useMutation(QlabService.method.createSlot);
  const clockIn = useMutation(QlabService.method.clockIn);
  const clockOut = useMutation(QlabService.method.clockOut);
  const cancelSlot = useMutation(QlabService.method.cancelSlot);
  const poke = useMutation(QlabService.method.pokeOccupant);
  const forceClockOut = useMutation(QlabService.method.forceClockOut);
  const forceNoShow = useMutation(QlabService.method.forceNoShow);
  const mutations = [createSlot, clockIn, clockOut, cancelSlot, poke, forceClockOut, forceNoShow];
  const pending = mutations.some((m) => m.isPending);
  const actionError = mutations.find((m) => m.error)?.error;

  // A ticking clock so client-derived flags (overrun, the reach floor) recompute as time
  // passes even while the page is idle — the schedule itself only refetches on a mutation
  // (and, next phase, on an SSE push). 30s granularity is plenty for minute-scale slots.
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 30_000);
    return () => clearInterval(id);
  }, []);

  // notice: a transient confirmation for actions whose row then disappears (cancel /
  // clock out), so the change doesn't happen silently. Cleared when the next action runs.
  const [notice, setNotice] = useState<string | null>(null);

  const poolResources = useMemo(
    () => (workspace?.resources ?? []).filter((r) => r.poolId === poolId),
    [workspace, poolId],
  );
  // The running-grid cells: one per resource, labelled once here (generic for now; named
  // resources later). The view matches active slots to these by id, never by label.
  const resources = useMemo<ResourceCell[]>(
    () => poolResources.map((r, i) => ({ id: r.id, label: `Hood ${i + 1}` })),
    [poolResources],
  );
  const memberLabel = useMemo(() => {
    const m = new Map<string, string>();
    for (const mem of workspace?.members ?? []) {
      m.set(mem.userId, mem.name || mem.email);
    }
    return m;
  }, [workspace]);

  const rows = useMemo(
    () =>
      resultToRows(data?.result, {
        currentUserId: actingUserId ?? "",
        now,
        memberLabel,
      }),
    [data, actingUserId, now, memberLabel],
  );

  // run fires a mutation, clears any stale notice, then refetches; errors surface via the
  // mutation's own state. An optional doneMsg confirms an action whose row then vanishes.
  async function run(fn: () => Promise<unknown>, doneMsg?: string) {
    setNotice(null);
    try {
      await fn();
      await refetch();
      if (doneMsg) {
        setNotice(doneMsg);
      }
    } catch {
      /* surfaced via actionError */
    }
  }

  const handlers = {
    onClockIn: (slotId: string) => void run(() => clockIn.mutateAsync({ slotId })),
    onClockOut: (slotId: string) =>
      void run(() => clockOut.mutateAsync({ slotId }), "Clocked out — slot completed."),
    onCancel: (slotId: string) =>
      void run(() => cancelSlot.mutateAsync({ slotId }), "Booking cancelled."),
    onPoke: (slotId: string) => void run(() => poke.mutateAsync({ slotId })),
    onForceClockOut: (slotId: string) => void run(() => forceClockOut.mutateAsync({ slotId })),
    onForceNoShow: (slotId: string) => void run(() => forceNoShow.mutateAsync({ slotId })),
    onBook: () => {
      if (!poolId) {
        return;
      }
      void run(() =>
        createSlot.mutateAsync({
          resourcePoolId: poolId,
          desiredStart: timestampFromDate(new Date()),
          lookaheadMinutes: 0,
          durationMinutes: 30,
          note: "",
        }),
      );
    },
  };

  const poolName = workspace?.pools.find((p) => p.id === poolId)?.name ?? "Pool";

  if (isLoading) {
    return <p className="text-muted text-sm">Loading schedule…</p>;
  }
  if (error) {
    return <p className="text-danger text-sm">Failed to load schedule — {errMessage(error)}</p>;
  }

  return (
    <div className="flex flex-col gap-2">
      {actionError && <p className="text-danger text-sm">{errMessage(actionError)}</p>}
      {notice && <p className="text-muted text-sm">{notice}</p>}
      <PoolView
        poolName={poolName}
        resources={resources}
        rows={rows}
        pending={pending}
        {...handlers}
      />
    </div>
  );
}
