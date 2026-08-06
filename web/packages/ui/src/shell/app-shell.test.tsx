// @vitest-environment jsdom
/**
 * The top bar as a set of slots.
 *
 * The clock, the theme switcher and the identity badge all arrive as nodes
 * rather than as props describing themselves. That is what keeps this file
 * from growing one prop per control anybody wants in the bar, and it is why
 * the bar can carry a ticking clock without knowing what a second is.
 */

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
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

  it("renders a context with options as something that opens", () => {
    const onChange = vi.fn();
    render(
      <TopBar
        contexts={[
          {
            label: "case",
            value: "nmdl_a",
            options: [
              { value: "nmdl_a", label: "PEGASE 2869" },
              { value: "nmdl_b", label: "IEEE 118" },
            ],
            onChange,
          },
        ]}
        clock={<span>18:14:07 Z</span>}
      />,
    );

    fireEvent.click(screen.getByRole("combobox", { name: "case" }));
    fireEvent.click(screen.getByRole("option", { name: /IEEE 118/ }));

    expect(onChange).toHaveBeenCalledExactlyOnceWith("nmdl_b");
  });

  it("renders a context without options as text, with no chevron behind it", () => {
    // The bar used to draw `▾` from a `menu: true` boolean with nothing
    // behind it. The two now travel together, so that is unexpressible.
    render(<TopBar contexts={[{ label: "case", value: "PEGASE 2869" }]} clock={<span>—</span>} />);

    expect(screen.getByText("PEGASE 2869")).toBeTruthy();
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(screen.queryByText("▾")).toBeNull();
  });

  it("leaves out the badge cell when there is no account to put in it", () => {
    // An empty bordered cell reads as a control that failed to render.
    render(<TopBar contexts={[]} clock={<span>18:14:07 Z</span>} />);

    expect(screen.queryByRole("button")).toBeNull();
  });
});
