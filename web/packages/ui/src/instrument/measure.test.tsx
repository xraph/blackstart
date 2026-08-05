// @vitest-environment jsdom
/**
 * A legend for a measured quantity.
 *
 * The console's rule is that a legend prints the class breaks it is actually
 * quantised to — a ramp without numbers says one thing is worse than another
 * and nothing about whether either is worth acting on.
 *
 * The direction is the part worth asserting. Ampacity is headroom, so a small
 * value is the severe one; loading is a burden, so a large one is. A legend
 * that runs the same way for both tells the reader a derated line is fine.
 */

import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MeasureRamp } from "./measure";

afterEach(cleanup);

const AMPACITY = {
  label: "Ampacity",
  unit: "A",
  breaks: [800, 1000, 1200, 1400] as const,
  severity: "low" as const,
};

const LOADING = {
  label: "Loading",
  unit: "N/m",
  breaks: [2, 5, 10, 20] as const,
  severity: "high" as const,
};

/** The bucket each swatch carries, left to right. */
function swatchOrder(): number[] {
  return Array.from(document.querySelectorAll("[data-bucket]")).map((el) =>
    Number(el.getAttribute("data-bucket")),
  );
}

describe("MeasureRamp", () => {
  it("names the unit the breaks are in", () => {
    render(<MeasureRamp {...AMPACITY} />);

    expect(screen.getByText(/\bA\b/)).toBeTruthy();
  });

  it("prints every break rather than an unlabelled gradient", () => {
    render(<MeasureRamp {...LOADING} />);

    for (const b of LOADING.breaks) {
      expect(screen.getByText(String(b))).toBeTruthy();
    }
  });

  it("puts the severe end on the left when a small value is the bad one", () => {
    // Ampacity ascends left to right like any axis, so the red belongs at the
    // low end. Running the ramp the other way draws a collapsed rating green.
    render(<MeasureRamp {...AMPACITY} />);

    expect(swatchOrder()).toEqual([4, 3, 2, 1, 0]);
  });

  it("puts the severe end on the right when a large value is the bad one", () => {
    render(<MeasureRamp {...LOADING} />);

    expect(swatchOrder()).toEqual([0, 1, 2, 3, 4]);
  });

  it("marks what is drawn for a span with no value", () => {
    // The unrated swatch sits outside the ramp, because "not computed" is not
    // a point on a scale of severity.
    render(<MeasureRamp {...AMPACITY} />);

    expect(screen.getByText(/unrated/i)).toBeTruthy();
  });
});
