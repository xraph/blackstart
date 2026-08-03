// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { BottomSheet, TabBar } from "./responsive";

beforeEach(() => {
  vi.stubGlobal("matchMedia", (q: string) => ({
    matches: false,
    media: q,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
});

// Auto-cleanup only fires with `globals: true`, which this config does not
// set. Without it every render accumulates in the same document and queries
// start matching the previous test's markup.
afterEach(cleanup);

describe("BottomSheet", () => {
  it("renders its children when open", () => {
    render(
      <BottomSheet title="Ranked contingencies" open onOpenChange={() => {}}>
        <p>rank 1</p>
      </BottomSheet>,
    );
    expect(screen.getByText("rank 1")).toBeTruthy();
  });

  it("stays reachable when closed, so the sheet advertises itself", () => {
    render(
      <BottomSheet title="Ranked contingencies" open={false} onOpenChange={() => {}}>
        <p>rank 1</p>
      </BottomSheet>,
    );
    const handle = screen.getByRole("button", { name: /ranked contingencies/i });
    expect(handle.getAttribute("aria-expanded")).toBe("false");
  });

  it("hides the body from assistive tech when collapsed rather than leaving it focusable", () => {
    render(
      <BottomSheet title="Ranked contingencies" open={false} onOpenChange={() => {}}>
        <button type="button">open element</button>
      </BottomSheet>,
    );
    // A collapsed sheet that still exposes its controls is a keyboard trap
    // with no visible target.
    expect(screen.queryByRole("button", { name: "open element" })).toBeNull();
  });

  it("shows a peek row when collapsed, so the sheet says what is under it", () => {
    render(
      <BottomSheet
        title="Ranked contingencies"
        open={false}
        onOpenChange={() => {}}
        peek={<span>TULIA — LEVELLAND</span>}
      >
        <p>rank 1</p>
      </BottomSheet>,
    );
    expect(screen.getByText("TULIA — LEVELLAND")).toBeTruthy();
  });
});

describe("TabBar", () => {
  const items = [
    { id: "console", label: "Console", icon: null },
    { id: "runs", label: "Runs", icon: null },
  ];

  it("marks the active tab for assistive technology, not only visually", () => {
    render(<TabBar active="console" onSelect={() => {}} items={items} />);
    expect(screen.getByRole("tab", { name: "Console" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("tab", { name: "Runs" }).getAttribute("aria-selected")).toBe("false");
  });

  it("reports the selected id", () => {
    const onSelect = vi.fn();
    render(<TabBar active="console" onSelect={onSelect} items={items} />);
    screen.getByRole("tab", { name: "Runs" }).click();
    expect(onSelect).toHaveBeenCalledWith("runs");
  });
});
