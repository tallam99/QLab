import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { ConnectError } from "@connectrpc/connect";
import { useQuery } from "@connectrpc/connect-query";
import { QlabService } from "../protogen/qlab/v1/service_pb";
import { SlotStatus } from "../protogen/qlab/v1/types_pb";
import { useSession } from "../session/SessionProvider";

// formatStart renders a slot's placed start, falling back to its desired start.
function formatStart(actual?: Timestamp, desired?: Timestamp): string {
  const ts = actual ?? desired;
  return ts ? timestampDate(ts).toLocaleString() : "—";
}

// SlotList renders the one real authenticated API call for Phase 9: ListSlots for
// the selected pool, scoped to the caller's lab by the transport's headers.
export function SlotList() {
  const { selection, canQuery } = useSession();
  const poolId = selection?.poolId ?? "";

  const { data, error, isLoading } = useQuery(
    QlabService.method.listSlots,
    { resourcePoolId: poolId },
    { enabled: canQuery },
  );

  if (isLoading) {
    return <p className="text-slate-400">Loading slots…</p>;
  }
  if (error) {
    const message =
      error instanceof ConnectError ? `${error.code}: ${error.rawMessage}` : String(error);
    return <p className="text-red-400">Failed to load slots — {message}</p>;
  }

  const slots = data?.slots ?? [];
  if (slots.length === 0) {
    return <p className="text-slate-400">No slots in this pool yet.</p>;
  }

  return (
    <table className="w-full text-left font-mono text-sm">
      <thead className="text-slate-400">
        <tr>
          <th className="py-1 pr-4">#</th>
          <th className="py-1 pr-4">Status</th>
          <th className="py-1 pr-4">Start</th>
          <th className="py-1 pr-4">Duration</th>
        </tr>
      </thead>
      <tbody>
        {slots.map((slot) => (
          <tr key={slot.id} className="border-t border-slate-700">
            <td className="py-1 pr-4">{slot.slotPriority}</td>
            <td className="py-1 pr-4">{SlotStatus[slot.status]}</td>
            <td className="py-1 pr-4">{formatStart(slot.actualStart, slot.desiredStart)}</td>
            <td className="py-1 pr-4">{slot.durationMinutes}m</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
