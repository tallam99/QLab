import { type FormEvent, useState } from "react";
import { useSession } from "../session/SessionProvider";

// DevTokenPanel is the staging/local act-as path (decision 0008): paste an
// operator-minted ID token plus the lab + pool to act in, without the Google
// OAuth dance. The token is optional — when signed in via Google, leave it blank
// and only the lab/pool selection is applied. There is no public ListPools RPC
// yet (Phase 10), so the ids are entered from the operator ProvisionLab response.
export function DevTokenPanel() {
  const { setSelection, setManualToken, selection, manualToken, clear } = useSession();
  const [labId, setLabId] = useState(selection?.labId ?? "");
  const [poolId, setPoolId] = useState(selection?.poolId ?? "");
  const [token, setToken] = useState("");

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    setManualToken(token.trim() === "" ? null : token.trim());
    setSelection({ labId: labId.trim(), poolId: poolId.trim() });
  }

  return (
    <form
      onSubmit={onSubmit}
      className="flex flex-col gap-2 rounded border border-slate-700 bg-slate-800/50 p-3"
    >
      <p className="text-xs uppercase tracking-wide text-slate-500">Dev session</p>
      <label className="flex flex-col gap-1 text-xs text-slate-400">
        Lab ID
        <input
          className="rounded bg-slate-900 px-2 py-1 font-mono text-sm text-slate-100"
          value={labId}
          onChange={(e) => setLabId(e.target.value)}
          placeholder="lab uuid"
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-slate-400">
        Pool ID
        <input
          className="rounded bg-slate-900 px-2 py-1 font-mono text-sm text-slate-100"
          value={poolId}
          onChange={(e) => setPoolId(e.target.value)}
          placeholder="resource pool uuid"
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-slate-400">
        Minted token (optional — leave blank to use Google sign-in)
        <input
          className="rounded bg-slate-900 px-2 py-1 font-mono text-sm text-slate-100"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder="operator-minted ID token"
        />
      </label>
      <div className="flex items-center gap-2">
        <button
          type="submit"
          className="rounded bg-teal-500 px-3 py-1 text-sm font-medium text-slate-900 hover:bg-teal-400"
          disabled={labId.trim() === "" || poolId.trim() === ""}
        >
          Apply
        </button>
        {(selection || manualToken) && (
          <button
            type="button"
            className="rounded bg-slate-700 px-3 py-1 text-sm hover:bg-slate-600"
            onClick={clear}
          >
            Clear
          </button>
        )}
      </div>
    </form>
  );
}
