import { type FormEvent, useState } from "react";
import { useWorkspace } from "../workspace/WorkspaceProvider";

// ProvisionModal creates a fresh demo workspace (a head + N members, one pool with M
// resources) via the operator surface. On success the new workspace is adopted and
// the modal closes. Preset counts keep the common case one click away.
export function ProvisionModal({ onClose }: { onClose: () => void }) {
  const { provision, busy } = useWorkspace();
  const [feature, setFeature] = useState("demo");
  const [memberCount, setMemberCount] = useState(3);
  const [resourceCount, setResourceCount] = useState(2);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    await provision(feature.trim() || "demo", memberCount, resourceCount);
    onClose();
  }

  return (
    <div className="fixed inset-0 z-10 flex items-center justify-center bg-black/60 p-4">
      <form
        onSubmit={onSubmit}
        className="flex w-full max-w-sm flex-col gap-3 rounded-lg border border-slate-700 bg-slate-800 p-5"
      >
        <h3 className="font-mono text-sm uppercase tracking-wide text-teal-400">New workspace</h3>
        <label className="flex flex-col gap-1 text-xs text-slate-400">
          Feature (workspace name)
          <input
            className="rounded bg-slate-900 px-2 py-1 text-sm text-slate-100"
            value={feature}
            onChange={(e) => setFeature(e.target.value)}
            placeholder="demo"
          />
        </label>
        <label className="flex flex-col gap-1 text-xs text-slate-400">
          Members (in addition to the head)
          <input
            type="number"
            min={0}
            max={20}
            className="rounded bg-slate-900 px-2 py-1 text-sm text-slate-100"
            value={memberCount}
            onChange={(e) => setMemberCount(Number(e.target.value))}
          />
        </label>
        <label className="flex flex-col gap-1 text-xs text-slate-400">
          Resources in the pool
          <input
            type="number"
            min={1}
            max={20}
            className="rounded bg-slate-900 px-2 py-1 text-sm text-slate-100"
            value={resourceCount}
            onChange={(e) => setResourceCount(Number(e.target.value))}
          />
        </label>
        <div className="mt-1 flex items-center justify-end gap-2">
          <button
            type="button"
            className="rounded bg-slate-700 px-3 py-1 text-sm hover:bg-slate-600"
            onClick={onClose}
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={busy}
            className="rounded bg-teal-500 px-3 py-1 text-sm font-medium text-slate-900 hover:bg-teal-400 disabled:opacity-50"
          >
            {busy ? "Provisioning…" : "Provision"}
          </button>
        </div>
      </form>
    </div>
  );
}
