import {
  type ReactNode,
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
} from "react";
import { setAuthHolder } from "../api/authHolder";
import { operatorClient } from "../api/operatorClient";
import type { LabSummary } from "../protogen/qlab/dev/v1/dev_pb";
import { type Member, type Workspace, workspaceFromGetLab, workspaceFromProvision } from "./model";

// WorkspaceProvider owns the dev switcher state: which demo workspace is loaded, who
// we're acting as, which pool is selected, and a per-user cache of minted ID tokens.
// It drives the operator surface (provision/mint/list/get) via the operator client,
// and feeds the api transport's auth holder the *acting-as* user's token + lab — so
// the public API (SlotList, slot mutations) runs as whoever we've switched to.
interface WorkspaceValue {
  workspace: Workspace | null;
  actingUserId: string | null;
  poolId: string | null;
  actingMember: Member | null;
  // canQuery is true once a workspace is loaded, a user is selected to act as, and a
  // pool is chosen — i.e. the api transport has a token + lab + pool to work with.
  canQuery: boolean;
  // busy is true while an operator call (provision/mint/load) is in flight.
  busy: boolean;
  error: string | null;
  provision: (feature: string, memberCount: number, resourceCount: number) => Promise<void>;
  loadWorkspace: (labId: string) => Promise<void>;
  listWorkspaces: () => Promise<LabSummary[]>;
  actAs: (userId: string) => Promise<void>;
  selectPool: (poolId: string) => void;
  reset: () => void;
}

const WorkspaceContext = createContext<WorkspaceValue | null>(null);

export function WorkspaceProvider({ children }: { children: ReactNode }) {
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [actingUserId, setActingUserId] = useState<string | null>(null);
  const [poolId, setPoolId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Minted ID tokens per acting-as user. Switching back to a user we've already acted
  // as is then instant — no re-mint — which is the whole point of the switcher. A ref
  // (not state) because the interceptor reads it lazily; mutating it needn't re-render.
  const tokenCache = useRef<Map<string, string>>(new Map());

  // Feed the api transport's auth holder synchronously during render (the same reason
  // SessionProvider does: a child query fired on this switch must not read a stale
  // token/lab). getToken returns the acting-as user's cached minted token.
  setAuthHolder({
    getToken: async () => (actingUserId ? (tokenCache.current.get(actingUserId) ?? null) : null),
    labId: workspace?.labId ?? null,
  });

  // run wraps an operator call with the busy flag and error capture, so every action
  // shares one in-flight/error convention.
  const run = useCallback(async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, []);

  // Loading a new/different workspace invalidates the act-as token cache (its tokens
  // belong to the previous lab's users) and clears the selection.
  const adopt = useCallback((ws: Workspace) => {
    tokenCache.current.clear();
    setWorkspace(ws);
    setActingUserId(null);
    setPoolId(ws.pools[0]?.id ?? null);
  }, []);

  const provision = useCallback(
    (feature: string, memberCount: number, resourceCount: number) =>
      run(async () => {
        const res = await operatorClient.provisionLab({ feature, memberCount, resourceCount });
        adopt(workspaceFromProvision(res));
      }),
    [run, adopt],
  );

  const loadWorkspace = useCallback(
    (labId: string) =>
      run(async () => {
        adopt(workspaceFromGetLab(await operatorClient.getLab({ labId })));
      }),
    [run, adopt],
  );

  const listWorkspaces = useCallback(async () => {
    return (await operatorClient.listLabs({ feature: "" })).labs;
  }, []);

  const actAs = useCallback(
    (userId: string) =>
      run(async () => {
        if (!tokenCache.current.has(userId)) {
          const res = await operatorClient.mintToken({ userId });
          tokenCache.current.set(userId, res.idToken);
        }
        setActingUserId(userId);
      }),
    [run],
  );

  const selectPool = useCallback((id: string) => setPoolId(id), []);

  const reset = useCallback(() => {
    tokenCache.current.clear();
    setWorkspace(null);
    setActingUserId(null);
    setPoolId(null);
    setError(null);
  }, []);

  const value = useMemo<WorkspaceValue>(() => {
    const actingMember = workspace?.members.find((m) => m.userId === actingUserId) ?? null;
    return {
      workspace,
      actingUserId,
      poolId,
      actingMember,
      canQuery: workspace !== null && actingUserId !== null && poolId !== null,
      busy,
      error,
      provision,
      loadWorkspace,
      listWorkspaces,
      actAs,
      selectPool,
      reset,
    };
  }, [
    workspace,
    actingUserId,
    poolId,
    busy,
    error,
    provision,
    loadWorkspace,
    listWorkspaces,
    actAs,
    selectPool,
    reset,
  ]);

  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}

export function useWorkspace(): WorkspaceValue {
  const value = useContext(WorkspaceContext);
  if (value === null) {
    throw new Error("useWorkspace must be used within a WorkspaceProvider");
  }
  return value;
}
