import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { getAuthHolder } from "../api/authHolder";
import { SessionProvider, useSession } from "./SessionProvider";

// Capture onAuthStateChanged's callback so tests can drive the Firebase user, and
// stub the Firebase module so no real SDK or VITE_FIREBASE_* env is needed.
const fb = vi.hoisted(() => ({
  authCb: null as ((user: unknown) => void) | null,
  signInWithPopup: vi.fn(() => Promise.resolve()),
  signOut: vi.fn(() => Promise.resolve()),
}));

vi.mock("../firebase", () => ({ auth: {}, googleProvider: {} }));
vi.mock("firebase/auth", () => ({
  onAuthStateChanged: (_auth: unknown, cb: (user: unknown) => void) => {
    fb.authCb = cb;
    return () => {};
  },
  signInWithPopup: fb.signInWithPopup,
  signOut: fb.signOut,
}));

const googleUser = { email: "head@qlab.dev", uid: "uid-1", getIdToken: async () => "google-token" };

function setup() {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <SessionProvider>{children}</SessionProvider>
  );
  return renderHook(() => useSession(), { wrapper });
}

// SessionProvider owns all auth + selection state and feeds the transport's auth
// holder; these tests pin the gating, the credential precedence the dev panel
// relies on, and that the holder is kept in sync — none of which was covered.
describe("SessionProvider", () => {
  beforeEach(() => {
    fb.authCb = null;
    fb.signInWithPopup.mockClear();
    fb.signOut.mockClear();
  });

  // canQuery needs both a credential and a selection; initializing flips once
  // Firebase first reports auth state.
  it("gates canQuery on a credential and a selection", () => {
    const { result } = setup();
    expect(result.current.initializing).toBe(true);

    act(() => fb.authCb?.(null));
    expect(result.current.initializing).toBe(false);
    expect(result.current.canQuery).toBe(false);

    act(() => result.current.setManualToken("minted"));
    expect(result.current.canQuery).toBe(false); // still no selection
    act(() => result.current.setSelection({ labId: "lab-1", poolId: "pool-1" }));
    expect(result.current.canQuery).toBe(true);
  });

  // The holder the transport reads reflects the selected lab and minted token.
  it("mirrors the minted token and selected lab into the auth holder", async () => {
    const { result } = setup();
    act(() => fb.authCb?.(null));
    act(() => {
      result.current.setManualToken("minted");
      result.current.setSelection({ labId: "lab-1", poolId: "pool-1" });
    });

    expect(getAuthHolder().labId).toBe("lab-1");
    await expect(getAuthHolder().getToken()).resolves.toBe("minted");
  });

  // A pasted minted token takes precedence over a live Google session (decision
  // 0008 act-as) — the behavior the dev panel's "active credential" line warns about.
  it("prefers a manual token over the Firebase user", async () => {
    const { result } = setup();
    act(() => fb.authCb?.(googleUser));
    act(() => result.current.setSelection({ labId: "lab-1", poolId: "pool-1" }));
    await expect(getAuthHolder().getToken()).resolves.toBe("google-token");

    act(() => result.current.setManualToken("minted"));
    await expect(getAuthHolder().getToken()).resolves.toBe("minted");
  });

  // clear() drops the act-as token and selection but leaves the Google session, so
  // the holder falls back to the Firebase token.
  it("clear() resets the manual token and selection", async () => {
    const { result } = setup();
    act(() => fb.authCb?.(googleUser));
    act(() => {
      result.current.setManualToken("minted");
      result.current.setSelection({ labId: "lab-1", poolId: "pool-1" });
    });

    act(() => result.current.clear());
    expect(result.current.selection).toBeNull();
    expect(result.current.manualToken).toBeNull();
    await expect(getAuthHolder().getToken()).resolves.toBe("google-token");
  });
});
