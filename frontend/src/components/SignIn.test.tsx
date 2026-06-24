import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { SignIn } from "./SignIn";

const signInWithGoogle = vi.fn(() => Promise.resolve());
const signOut = vi.fn(() => Promise.resolve());
// Mutated per-test to drive SignIn off a fixed session without pulling in Firebase.
let sessionValue: Record<string, unknown>;

vi.mock("../session/SessionProvider", () => ({
  useSession: () => sessionValue,
}));

// SignIn is the production auth control; verify the signed-out click path (and its
// error surfacing) and the signed-in user + sign-out path, since none of this
// interactive wiring was covered before.
describe("SignIn", () => {
  it("invokes Google sign-in and surfaces a failure as text", async () => {
    const failing = vi.fn(() => Promise.reject(new Error("popup closed")));
    sessionValue = { user: null, signInWithGoogle: failing, signOut };
    const user = userEvent.setup();
    render(<SignIn />);

    await user.click(screen.getByRole("button", { name: /sign in with google/i }));
    expect(failing).toHaveBeenCalledOnce();
    expect(await screen.findByText(/popup closed/i)).toBeInTheDocument();
  });

  it("shows the signed-in user and signs out", async () => {
    sessionValue = {
      user: { email: "head@qlab.dev", uid: "uid-1" },
      signInWithGoogle,
      signOut,
    };
    const user = userEvent.setup();
    render(<SignIn />);

    expect(screen.getByText("head@qlab.dev")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /sign out/i }));
    expect(signOut).toHaveBeenCalledOnce();
  });
});
