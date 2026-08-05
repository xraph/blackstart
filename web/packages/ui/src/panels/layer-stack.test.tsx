// @vitest-environment jsdom
/**
 * A row for a layer this build cannot draw.
 *
 * The stack lists the intended shape of the system, not only its current
 * state, so the Phase 7 layers stay visible. What must not happen is a control
 * that looks live and does nothing — a checkbox that never checks reads as a
 * broken panel rather than as absent data.
 */

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { LayerStack } from "@blackstart/schema";
import { LayerStackPanel, layerSummary } from "./layer-stack";

afterEach(cleanup);

const STACK = LayerStack.parse([
  { id: "network", label: "Network", state: "on", opacity: 1, source: "ACTIVSg2000 v3" },
  { id: "clouds", label: "Clouds", state: "unavailable", opacity: 0, source: "HRRR conus" },
]);

describe("LayerStackPanel", () => {
  it("offers a toggle for a layer that can be drawn", () => {
    render(<LayerStackPanel layers={STACK} />);

    expect(screen.getByRole("checkbox", { name: "Network" })).toBeTruthy();
  });

  it("offers no toggle for a layer the build cannot draw", () => {
    render(<LayerStackPanel layers={STACK} />);

    expect(screen.queryByRole("checkbox", { name: "Clouds" })).toBeNull();
  });

  it("offers no opacity control for a layer the build cannot draw", () => {
    // A slider over nothing is worse than no slider: moving it implies the
    // layer is there and merely faint.
    render(<LayerStackPanel layers={STACK} />);

    expect(screen.queryByLabelText("Clouds opacity")).toBeNull();
    expect(screen.getByLabelText("Network opacity")).toBeTruthy();
  });

  it("says why the row is inert rather than leaving it looking broken", () => {
    render(<LayerStackPanel layers={STACK} />);

    expect(screen.getByText(/not in this build/i)).toBeTruthy();
  });

  it("cannot be toggled through the row itself", () => {
    const onToggle = vi.fn();
    render(<LayerStackPanel layers={STACK} onToggle={onToggle} />);

    screen.getByText("Clouds").click();

    expect(onToggle).not.toHaveBeenCalled();
  });
});

describe("layerSummary", () => {
  it("counts only the layers actually contributing pixels", () => {
    expect(layerSummary(STACK)).toBe("1 / 2");
  });
});
