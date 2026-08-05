/**
 * The layer stack, built from what the platform actually answered.
 *
 * It was a fixture listing seven layers, five of which describe Phase 7 work
 * no build can draw. The rows stay — the stack shows the intended shape of the
 * system as well as its current state — but they say so, and the three that
 * are real carry the dataset versions they are really drawing.
 */

import { describe, expect, it } from "vitest";
import { activeLayerCount } from "@blackstart/schema";

import { AMPACITY, LOADING } from "../api/hazard";
import { hazardLayerStack } from "./layers";

const STATE = {
  encoding: AMPACITY,
  issuance: "2026-08-04T03:00:00Z",
  rated: 21,
  unrated: 317,
  network: "ACTIVSg2000 v3",
};

describe("hazardLayerStack", () => {
  it("keeps every row the console has ever listed", () => {
    // Eight, not the fixture's seven: the single "risk surface" row is
    // replaced by the two quantities the platform actually serves.
    expect(hazardLayerStack(STATE)).toHaveLength(8);
  });

  it("marks the layers no build can draw yet", () => {
    const absent = hazardLayerStack(STATE).filter((l) => l.state === "unavailable");

    expect(absent.map((l) => l.id)).toEqual(["clouds", "wind", "precip", "terrain", "veg"]);
  });

  it("draws only the quantity that was asked for", () => {
    const stack = hazardLayerStack(STATE);

    expect(stack.find((l) => l.id === "ampacity")?.state).toBe("on");
    expect(stack.find((l) => l.id === "loading")?.state).toBe("off");
  });

  it("follows the encoding rather than hard-coding which is on", () => {
    const stack = hazardLayerStack({ ...STATE, encoding: LOADING });

    expect(stack.find((l) => l.id === "loading")?.state).toBe("on");
    expect(stack.find((l) => l.id === "ampacity")?.state).toBe("off");
  });

  it("names the issuance the values were computed from", () => {
    // The row is what makes a screenshot self-describing. A hazard layer whose
    // source says only "hazard" cannot be traced back to a forecast cycle.
    const source = hazardLayerStack(STATE).find((l) => l.id === "ampacity")?.source;

    expect(source).toContain("08-04 03Z");
  });

  it("keeps the row short enough for the column it is printed in", () => {
    // The panel gives the source about eighteen monospace characters. A longer
    // string wraps, and the row is a fixed 30px, so it overlaps its
    // neighbours — which was how the full ISO instant rendered.
    for (const layer of hazardLayerStack(STATE)) {
      expect(layer.source.length, `${layer.id}: "${layer.source}"`).toBeLessThanOrEqual(18);
    }
  });

  it("says so plainly when no pass has run", () => {
    // Distinct from a pass that rated nothing, and the reader needs to know
    // which: one is a missing job, the other a missing conductor.
    const source = hazardLayerStack({ ...STATE, issuance: null, rated: 0 }).find(
      (l) => l.id === "ampacity",
    )?.source;

    expect(source).toMatch(/no pass/i);
  });

  it("counts only the rows contributing pixels", () => {
    // Three real layers, one of the two quantities off, five unavailable.
    expect(activeLayerCount(hazardLayerStack(STATE))).toBe(2);
  });
});
