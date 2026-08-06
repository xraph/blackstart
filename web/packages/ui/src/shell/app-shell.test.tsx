// @vitest-environment jsdom
/**
 * The top bar as a set of slots.
 *
 * The clock, the theme switcher and the identity badge all arrive as nodes
 * rather than as props describing themselves. That is what keeps this file
 * from growing one prop per control anybody wants in the bar, and it is why
 * the bar can carry a ticking clock without knowing what a second is.
 */

import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { TopBar } from "./app-shell";

afterEach(cleanup);

describe("TopBar", () => {
  it("renders chrome handed to it without knowing what it is", () => {
    render(
      <TopBar
        contexts={[]}
        clock={<span>18:14:07 Z</span>}
        aside={<button type="button">Colour theme</button>}
        account={<button type="button">Account</button>}
      />,
    );

    expect(screen.getByText("18:14:07 Z")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Colour theme" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Account" })).toBeTruthy();
  });

  it("leaves out the badge cell when there is no account to put in it", () => {
    // An empty bordered cell reads as a control that failed to render.
    render(<TopBar contexts={[]} clock={<span>18:14:07 Z</span>} />);

    expect(screen.queryByRole("button")).toBeNull();
  });
});
