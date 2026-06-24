import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ComingSoon } from "./ComingSoon";

// ComingSoon is the production placeholder. Its whole job is to be inert — no
// sign-in, nothing interactive — so a prod build exposes neither the dev tool nor a
// path to create a stray auth identity. Guard that it stays that way.
describe("ComingSoon", () => {
  it("renders a neutral placeholder with nothing interactive", () => {
    render(<ComingSoon />);
    expect(screen.getByText(/coming soon/i)).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });
});
