import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
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

function setup() {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <SessionProvider>{children}</SessionProvider>
  );
  return renderHook(() => useSession(), { wrapper });
}

// SessionProvider now owns only the operator identity (the acting-as credentials moved
// to WorkspaceProvider); these tests pin that it tracks the Firebase user, flips
// initializing once, and routes sign-in/out through the SDK.
describe("SessionProvider", () => {
  beforeEach(() => {
    fb.authCb = null;
    fb.signInWithPopup.mockClear();
    fb.signOut.mockClear();
  });

  it("tracks the operator user and flips initializing once auth state arrives", () => {
    const { result } = setup();
    expect(result.current.initializing).toBe(true);
    expect(result.current.user).toBeNull();

    act(() => fb.authCb?.({ email: "operator@qlab.dev" }));
    expect(result.current.initializing).toBe(false);
    expect(result.current.user).toEqual({ email: "operator@qlab.dev" });
  });

  it("routes sign-in and sign-out through Firebase", async () => {
    const { result } = setup();
    act(() => fb.authCb?.(null));

    await act(async () => {
      await result.current.signInWithGoogle();
    });
    expect(fb.signInWithPopup).toHaveBeenCalledOnce();

    await act(async () => {
      await result.current.signOut();
    });
    expect(fb.signOut).toHaveBeenCalledOnce();
  });
});
