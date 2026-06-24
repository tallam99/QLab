import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { timestampDate, timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { useMemo } from "react";
import { QlabService } from "../protogen/qlab/v1/service_pb";
import type { RescheduleResult } from "../protogen/qlab/v1/types_pb";
import { SlotStatus } from "../protogen/qlab/v1/types_pb";
import { useWorkspace } from "../workspace/WorkspaceProvider";
import { PoolView } from "./PoolView";
import type { SlotRow } from "./slot-ui";

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
  resourceLabel: Map<string, string>;
}

// resultToRows turns a GetSchedule RescheduleResult into the view's SlotRow[]. The
// engine supplies placement + reclaimable (via positions); overrun, earliestStart, and
// the resource label are derived here from data already on the slot.
export function resultToRows(result: RescheduleResult | undefined, ctx: MapContext): SlotRow[] {
  if (!result) {
    return [];
  }
  const posBySlot = new Map(result.positions.map((p) => [p.slotId, p]));
  // youAreNext is approximated as "you own the front of the queue" — the gate for the
  // poke/boot/reclaim actions. The backend enforces the real next-in-line check.
  const front = result.slots
    .filter((s) => s.status === SlotStatus.SCHEDULED)
    .reduce<{ userId: string; slotPriority: number } | null>(
      (min, s) => (!min || s.slotPriority < min.slotPriority ? s : min),
      null,
    );
  const youOwnFront = front ? front.userId === ctx.currentUserId : false;

  return result.slots.map((s) => {
    const start = toDate(s.actualStart) ?? toDate(s.desiredStart);
    const isYou = s.userId === ctx.currentUserId;
    const overrun =
      s.status === SlotStatus.ACTIVE &&
      !!start &&
      start.getTime() + s.durationMinutes * 60000 < ctx.now.getTime();
    const reclaimable = posBySlot.get(s.id)?.reclaimable ?? false;

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
      reclaimable,
      youAreNext: !isYou && (overrun || reclaimable) ? youOwnFront : false,
      resourceLabel:
        s.status === SlotStatus.ACTIVE ? ctx.resourceLabel.get(s.assignedResourceId) : undefined,
      earliestStart,
    };
  });
}

// PoolPanel is the container behind the product view: it loads the pool's schedule via
// the read-only GetSchedule, maps it to rows, and wires the seven mutating RPCs. Every
// action refetches so the queue reflects the engine's reschedule (the SSE stream will
// replace the refetch in the next phase).
export function PoolPanel() {
  const { poolId, canQuery, actingUserId, workspace } = useWorkspace();
  const { data, error, isLoading, refetch } = useQuery(
    QlabService.method.getSchedule,
    { resourcePoolId: poolId ?? "" },
    { enabled: canQuery },
  );

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

  const poolResources = useMemo(
    () => (workspace?.resources ?? []).filter((r) => r.poolId === poolId),
    [workspace, poolId],
  );
  const resourceLabel = useMemo(() => {
    const m = new Map<string, string>();
    poolResources.forEach((r, i) => m.set(r.id, `Hood ${i + 1}`));
    return m;
  }, [poolResources]);
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
        now: new Date(),
        resourceLabel,
        memberLabel,
      }),
    [data, actingUserId, resourceLabel, memberLabel],
  );

  // run fires a mutation then refetches; errors surface via the mutation's own state.
  async function run(fn: () => Promise<unknown>) {
    try {
      await fn();
      await refetch();
    } catch {
      /* surfaced via actionError */
    }
  }

  const handlers = {
    onClockIn: (slotId: string) => void run(() => clockIn.mutateAsync({ slotId })),
    onClockOut: (slotId: string) => void run(() => clockOut.mutateAsync({ slotId })),
    onCancel: (slotId: string) => void run(() => cancelSlot.mutateAsync({ slotId })),
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
      <PoolView
        poolName={poolName}
        resourceCount={poolResources.length}
        rows={rows}
        pending={pending}
        {...handlers}
      />
    </div>
  );
}
