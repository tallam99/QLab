import { LabRole } from "../protogen/qlab/v1/types_pb";
import { useWorkspace } from "../workspace/WorkspaceProvider";
import type { Member } from "../workspace/model";

// memberLabel shows who you'd be acting as: name (or email) plus role.
function memberLabel(m: Member): string {
  const who = m.name || m.email;
  const role = m.role === LabRole.HEAD ? "head" : "member";
  return `${who} (${role})`;
}

// ActAsSwitcher is the heart of the dev switcher: pick which seeded user to act as
// and which pool to schedule against. Switching users mints (or reuses a cached)
// token under the hood — no pasting, instant on a repeat — so the api transport runs
// every subsequent call as the selected user. Renders nothing until a workspace loads.
export function ActAsSwitcher() {
  const { workspace, actingUserId, actAs, poolId, selectPool, actingMember, busy } = useWorkspace();
  if (!workspace) {
    return null;
  }

  return (
    <section className="flex flex-wrap items-center gap-3 rounded border border-slate-700 bg-slate-800/50 p-3">
      <label className="flex items-center gap-2 text-xs text-slate-400">
        Act as
        <select
          className="rounded bg-slate-900 px-2 py-1 text-sm text-slate-100"
          value={actingUserId ?? ""}
          onChange={(e) => void actAs(e.target.value)}
          disabled={busy}
        >
          <option value="" disabled>
            Select a user…
          </option>
          {workspace.members.map((m) => (
            <option key={m.userId} value={m.userId}>
              {memberLabel(m)}
            </option>
          ))}
        </select>
      </label>
      <label className="flex items-center gap-2 text-xs text-slate-400">
        Pool
        <select
          className="rounded bg-slate-900 px-2 py-1 text-sm text-slate-100"
          value={poolId ?? ""}
          onChange={(e) => selectPool(e.target.value)}
        >
          {workspace.pools.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </label>
      {actingMember && (
        <span className="ml-auto text-sm text-slate-300">
          Acting as <span className="text-teal-400">{actingMember.name || actingMember.email}</span>
        </span>
      )}
    </section>
  );
}
