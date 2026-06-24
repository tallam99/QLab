import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { getAuthHolder } from "../api/authHolder";
import { WorkspaceProvider, useWorkspace } from "./WorkspaceProvider";

// Stub the imperative operator client so no transport/Firebase is needed. mintToken
// returns a token keyed to the user id so the cache behaviour is observable.
const ops = vi.hoisted(() => ({
  provisionLab: vi.fn(),
  mintToken: vi.fn(),
  listLabs: vi.fn(),
  getLab: vi.fn(),
}));
vi.mock("../api/operatorClient", () => ({ operatorClient: ops }));

// Mock the operator identity so tests can drive sign-out (a uid change) without
// pulling in Firebase. Defaults to a signed-in operator.
const session = vi.hoisted(() => ({ user: { uid: "op-1" } as { uid: string } | null }));
vi.mock("../session/SessionProvider", () => ({ useSession: () => ({ user: session.user }) }));

function setup() {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <WorkspaceProvider>{children}</WorkspaceProvider>
  );
  return renderHook(() => useWorkspace(), { wrapper });
}

const provisionResponse = {
  lab: { id: "lab-1", name: "demo" },
  pool: { id: "pool-1", name: "Hoods" },
  members: [
    { user: { id: "u1", email: "a@qlab.dev", firstName: "Ann", lastName: "" }, role: 1 },
    { user: { id: "u2", email: "b@qlab.dev", firstName: "Bo", lastName: "" }, role: 2 },
  ],
  resources: [],
};

describe("WorkspaceProvider", () => {
  beforeEach(() => {
    session.user = { uid: "op-1" };
    ops.provisionLab.mockReset();
    ops.mintToken.mockReset();
    ops.provisionLab.mockResolvedValue(provisionResponse);
    ops.mintToken.mockImplementation(async ({ userId }: { userId: string }) => ({
      idToken: `token-${userId}`,
      user: { id: userId },
    }));
  });

  // Provisioning adopts the workspace, selects its pool, and leaves no one acted-as
  // until the operator picks someone.
  it("provisions a workspace and selects its pool", async () => {
    const { result } = setup();
    await act(async () => {
      await result.current.provision("demo", 1, 1);
    });

    expect(result.current.workspace?.labId).toBe("lab-1");
    expect(result.current.workspace?.members).toHaveLength(2);
    expect(result.current.poolId).toBe("pool-1");
    expect(result.current.actingUserId).toBeNull();
    expect(result.current.canQuery).toBe(false); // no acting-as user yet
  });

  // The whole point of the switcher: a minted token is cached per user, so switching
  // back to someone you've already acted as does NOT mint again. The api auth holder
  // always reflects the current acting-as user's token + the workspace lab.
  it("caches minted tokens so switching back never re-mints", async () => {
    const { result } = setup();
    await act(async () => {
      await result.current.provision("demo", 1, 1);
    });

    await act(async () => {
      await result.current.actAs("u1");
    });
    await act(async () => {
      await result.current.actAs("u2");
    });
    await act(async () => {
      await result.current.actAs("u1"); // back to u1 — must reuse the cached token
    });

    expect(ops.mintToken).toHaveBeenCalledTimes(2); // u1 and u2 only
    expect(result.current.canQuery).toBe(true);
    expect(getAuthHolder().labId).toBe("lab-1");
    await expect(getAuthHolder().getToken()).resolves.toBe("token-u1");
  });

  // Signing out (or switching operator accounts) must drop the prior session entirely
  // — workspace, selection, AND the cached minted tokens — so the next operator can't
  // see or act on it. The api auth holder must stop yielding the old token.
  it("clears the workspace and token cache when the operator changes", async () => {
    const { result, rerender } = setup();
    await act(async () => {
      await result.current.provision("demo", 1, 1);
    });
    await act(async () => {
      await result.current.actAs("u1");
    });
    expect(result.current.canQuery).toBe(true);
    await expect(getAuthHolder().getToken()).resolves.toBe("token-u1");

    // Operator signs out → uid changes → WorkspaceProvider resets.
    await act(async () => {
      session.user = null;
      rerender();
    });

    expect(result.current.workspace).toBeNull();
    expect(result.current.actingUserId).toBeNull();
    expect(result.current.poolId).toBeNull();
    await expect(getAuthHolder().getToken()).resolves.toBeNull();
  });
});
