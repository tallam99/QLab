import { type Timestamp, timestampDate, timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { QlabService } from "../protogen/qlab/v1/service_pb";
import { SlotStatus } from "../protogen/qlab/v1/types_pb";
import { useWorkspace } from "../workspace/WorkspaceProvider";

// formatStart renders a slot's placed start, falling back to its desired start.
function formatStart(actual?: Timestamp, desired?: Timestamp): string {
  const ts = actual ?? desired;
  return ts ? timestampDate(ts).toLocaleString() : "—";
}

// errMessage shows a ConnectError as "<code-name>: <message>" (the code is a numeric
// enum on the wire) and anything else via String().
function errMessage(error: unknown): string {
  return error instanceof ConnectError ? `${Code[error.code]}: ${error.rawMessage}` : String(error);
}

// A slot can be cancelled only while it is still live (waiting or running); the
// terminal states have nothing to cancel.
function cancellable(status: SlotStatus): boolean {
  return status === SlotStatus.SCHEDULED || status === SlotStatus.ACTIVE;
}

// SlotList shows the selected pool's queue for whoever we're acting as and lets that
// user add or cancel a slot — enough to exercise the multi-user reflow (act as A,
// add; switch to B, add; cancel and watch the queue re-flow). Every call runs as the
// acting-as user via the api transport's auth holder, fed by WorkspaceProvider.
export function SlotList() {
  const { poolId, canQuery } = useWorkspace();
  const { data, error, isLoading, refetch } = useQuery(
    QlabService.method.listSlots,
    { resourcePoolId: poolId ?? "" },
    { enabled: canQuery },
  );
  const createSlot = useMutation(QlabService.method.createSlot);
  const cancelSlot = useMutation(QlabService.method.cancelSlot);
  const mutating = createSlot.isPending || cancelSlot.isPending;
  const actionError = createSlot.error ?? cancelSlot.error;

  async function addSlot() {
    if (!poolId) {
      return;
    }
    // A simple demo booking: starts now, 30 minutes, no earliness. The engine places
    // it; switching users and adding more exercises the reschedule.
    await createSlot.mutateAsync({
      resourcePoolId: poolId,
      desiredStart: timestampFromDate(new Date()),
      lookaheadMinutes: 0,
      durationMinutes: 30,
      note: "",
    });
    await refetch();
  }

  async function cancel(slotId: string) {
    await cancelSlot.mutateAsync({ slotId });
    await refetch();
  }

  const slots = data?.slots ?? [];

  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h2 className="font-mono text-sm uppercase tracking-wide text-slate-400">Slots</h2>
        <button
          type="button"
          disabled={!canQuery || mutating}
          onClick={() => void addSlot()}
          className="rounded bg-teal-500 px-3 py-1 text-sm font-medium text-slate-900 hover:bg-teal-400 disabled:opacity-50"
        >
          Add 30-min slot
        </button>
      </div>
      {actionError && <p className="text-sm text-red-400">{errMessage(actionError)}</p>}
      {isLoading ? (
        <p className="text-slate-400">Loading slots…</p>
      ) : error ? (
        <p className="text-red-400">Failed to load slots — {errMessage(error)}</p>
      ) : slots.length === 0 ? (
        <p className="text-slate-400">No slots in this pool yet.</p>
      ) : (
        <table className="w-full text-left font-mono text-sm">
          <thead className="text-slate-400">
            <tr>
              <th className="py-1 pr-4">#</th>
              <th className="py-1 pr-4">Status</th>
              <th className="py-1 pr-4">Start</th>
              <th className="py-1 pr-4">Duration</th>
              <th className="py-1" />
            </tr>
          </thead>
          <tbody>
            {slots.map((slot) => (
              <tr key={slot.id} className="border-t border-slate-700">
                <td className="py-1 pr-4">{slot.slotPriority}</td>
                <td className="py-1 pr-4">{SlotStatus[slot.status] ?? `status ${slot.status}`}</td>
                <td className="py-1 pr-4">{formatStart(slot.actualStart, slot.desiredStart)}</td>
                <td className="py-1 pr-4">{slot.durationMinutes}m</td>
                <td className="py-1 text-right">
                  {cancellable(slot.status) && (
                    <button
                      type="button"
                      disabled={mutating}
                      onClick={() => void cancel(slot.id)}
                      className="rounded bg-slate-700 px-2 py-0.5 text-xs hover:bg-slate-600 disabled:opacity-50"
                    >
                      Cancel
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
