import { useEffect, useState } from "react";
import type { LabSummary } from "../protogen/qlab/dev/v1/dev_pb";
import { useWorkspace } from "../workspace/WorkspaceProvider";
import { ProvisionModal } from "./ProvisionModal";

// WorkspacePicker is the operator's entry point: provision a new demo workspace, or
// load an existing one to operate in. Replaces pasting a lab id by hand — the lab
// list comes from the operator ListLabs. The roster/pool/act-as controls live in
// ActAsSwitcher, shown once a workspace is loaded.
export function WorkspacePicker() {
  const { workspace, loadWorkspace, listWorkspaces, busy } = useWorkspace();
  const [labs, setLabs] = useState<LabSummary[]>([]);
  const [selectedLabId, setSelectedLabId] = useState("");
  const [showModal, setShowModal] = useState(false);
  const [listError, setListError] = useState<string | null>(null);

  // Load the workspace list once on mount (the operator is signed in by the time this
  // renders — App gates it). refresh() is also called after provisioning a new one.
  async function refresh() {
    try {
      setLabs(await listWorkspaces());
      setListError(null);
    } catch (e) {
      setListError(e instanceof Error ? e.message : String(e));
    }
  }
  // biome-ignore lint/correctness/useExhaustiveDependencies: run once on mount.
  useEffect(() => {
    void refresh();
  }, []);

  return (
    <section className="flex flex-col gap-2 rounded border border-slate-700 bg-slate-800/50 p-3">
      <p className="text-xs uppercase tracking-wide text-slate-500">Workspace</p>
      <div className="flex flex-wrap items-center gap-2">
        <select
          className="rounded bg-slate-900 px-2 py-1 font-mono text-sm text-slate-100"
          value={selectedLabId}
          onChange={(e) => setSelectedLabId(e.target.value)}
        >
          <option value="">Load existing…</option>
          {labs.map((l) => (
            <option key={l.lab?.id} value={l.lab?.id}>
              {l.lab?.name} · {l.userCount}u/{l.resourceCount}r
            </option>
          ))}
        </select>
        <button
          type="button"
          disabled={selectedLabId === "" || busy}
          className="rounded bg-slate-700 px-3 py-1 text-sm hover:bg-slate-600 disabled:opacity-50"
          onClick={() => void loadWorkspace(selectedLabId)}
        >
          Load
        </button>
        <button
          type="button"
          className="rounded bg-teal-500 px-3 py-1 text-sm font-medium text-slate-900 hover:bg-teal-400"
          onClick={() => setShowModal(true)}
        >
          New workspace
        </button>
        {workspace && (
          <span className="ml-auto text-sm text-slate-300">
            In <span className="font-mono text-teal-400">{workspace.labName}</span>
          </span>
        )}
      </div>
      {listError && <span className="text-xs text-red-400">{listError}</span>}
      {showModal && (
        <ProvisionModal
          onClose={() => {
            setShowModal(false);
            void refresh();
          }}
        />
      )}
    </section>
  );
}
