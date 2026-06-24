import type {
  GetLabResponse,
  LabMember,
  ProvisionLabResponse,
} from "../protogen/qlab/dev/v1/dev_pb";
import type { LabRole } from "../protogen/qlab/v1/types_pb";

// The switcher's local view of a workspace, flattened from the operator proto
// responses so components don't carry proto shapes. A workspace is one demo lab plus
// its roster (who you can act as) and its pools (what you can schedule against).
export interface Member {
  userId: string;
  email: string;
  name: string;
  role: LabRole;
}

export interface Pool {
  id: string;
  name: string;
}

export interface Resource {
  id: string;
  poolId: string;
  name: string;
}

export interface Workspace {
  labId: string;
  labName: string;
  members: Member[];
  pools: Pool[];
  // resources across the lab; PoolView filters to its pool to label "Hood N" cells and
  // map an active slot's assigned resource id to a name.
  resources: Resource[];
}

function memberOf(m: LabMember): Member {
  const user = m.user;
  const name = [user?.firstName, user?.lastName].filter(Boolean).join(" ").trim();
  return { userId: user?.id ?? "", email: user?.email ?? "", name, role: m.role };
}

// ProvisionLab returns a single freshly-created pool; GetLab returns all of a lab's
// pools — hence the two converters.
function resourceOf(r: { id: string; resourcePoolId: string; name: string }): Resource {
  return { id: r.id, poolId: r.resourcePoolId, name: r.name };
}

export function workspaceFromProvision(res: ProvisionLabResponse): Workspace {
  return {
    labId: res.lab?.id ?? "",
    labName: res.lab?.name ?? "",
    members: res.members.map(memberOf),
    pools: res.pool ? [{ id: res.pool.id, name: res.pool.name }] : [],
    resources: res.resources.map(resourceOf),
  };
}

export function workspaceFromGetLab(res: GetLabResponse): Workspace {
  return {
    labId: res.lab?.id ?? "",
    labName: res.lab?.name ?? "",
    members: res.members.map(memberOf),
    pools: res.pools.map((p) => ({ id: p.id, name: p.name })),
    resources: res.resources.map(resourceOf),
  };
}
