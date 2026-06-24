import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DevTokenPanel } from "./DevTokenPanel";

const setSelection = vi.fn();
const setManualToken = vi.fn();

// Provide spy-backed session methods so the test asserts what the panel applies.
vi.mock("../session/SessionProvider", () => ({
  useSession: () => ({
    setSelection,
    setManualToken,
    selection: null,
    manualToken: null,
    clear: vi.fn(),
  }),
}));

// The dev panel is the staging act-as path; verify Apply pushes the lab/pool and a
// pasted token into the session, and treats a blank token as "use Google sign-in".
describe("DevTokenPanel", () => {
  it("applies a pasted token plus lab and pool", async () => {
    const user = userEvent.setup();
    render(<DevTokenPanel />);

    await user.type(screen.getByPlaceholderText("lab uuid"), "lab-123");
    await user.type(screen.getByPlaceholderText("resource pool uuid"), "pool-456");
    await user.type(screen.getByPlaceholderText("operator-minted ID token"), "minted-token");
    await user.click(screen.getByRole("button", { name: "Apply" }));

    expect(setManualToken).toHaveBeenCalledWith("minted-token");
    expect(setSelection).toHaveBeenCalledWith({ labId: "lab-123", poolId: "pool-456" });
  });

  it("clears the manual token when the token field is blank", async () => {
    const user = userEvent.setup();
    render(<DevTokenPanel />);

    await user.type(screen.getByPlaceholderText("lab uuid"), "lab-123");
    await user.type(screen.getByPlaceholderText("resource pool uuid"), "pool-456");
    await user.click(screen.getByRole("button", { name: "Apply" }));

    expect(setManualToken).toHaveBeenCalledWith(null);
  });
});
