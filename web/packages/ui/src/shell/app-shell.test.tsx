// @vitest-environment jsdom
/**
 * The top bar's identity badge.
 *
 * It has always printed initials. Signing out is the one action attached to
 * them, and it arrives as an optional prop so every existing caller renders
 * exactly as it did — a bar that grows a button nobody asked for would be a
 * change to screens this work is not touching.
 */

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { TopBar } from "./app-shell";

afterEach(cleanup);

describe("TopBar", () => {
  it("prints the initials as plain text when there is nothing to do with them", () => {
    render(<TopBar contexts={[]} clock="18:14:07 Z" initials="RR" />);

    expect(screen.getByText("RR")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("offers a named sign-out when one is given", () => {
    // Named, not just clickable: "RR" tells a screen reader nothing about what
    // pressing it does.
    const onSignOut = vi.fn();
    render(<TopBar contexts={[]} clock="18:14:07 Z" initials="RR" onSignOut={onSignOut} />);

    screen.getByRole("button", { name: /sign out/i }).click();

    expect(onSignOut).toHaveBeenCalledOnce();
  });
});
