/**
 * The fixture parses itself at import, so these tests are not re-checking the
 * schemas. They pin the properties the screens depend on being true of *this*
 * data — most importantly that it contains the cost-versus-probability
 * inversion the contingency rail exists to show.
 */

import { describe, expect, it } from "vitest";
import { CascadeRun, ContingencyList, ScenarioRun, chainCompleteness } from "@blackstart/schema";
import { CASCADE, CONTINGENCIES, FREQ_TRACES, INSPECTED, LAYERS, RUNS } from "./derecho";

describe("derecho fixture", () => {
  it("satisfies the contingency ordering invariant", () => {
    expect(ContingencyList.safeParse(CONTINGENCIES).success).toBe(true);
    expect(CONTINGENCIES.map((c) => c.rank)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12]);
  });

  it("contains a row that is likelier than the one above it", () => {
    // Without this the rail would be indistinguishable from a probability
    // sort, and the ranking would be making no claim at all.
    const inverted = CONTINGENCIES.some(
      (c, i) => i > 0 && c.probability > (CONTINGENCIES[i - 1]?.probability ?? 1),
    );
    expect(inverted).toBe(true);
  });

  it("satisfies the cascade invariants", () => {
    expect(CascadeRun.safeParse(CASCADE).success).toBe(true);
  });

  it("only references islands the run actually formed", () => {
    const formed = new Set(CASCADE.islands.map((i) => i.id));
    for (const e of CASCADE.events) {
      if (e.island !== undefined) expect(formed.has(e.island)).toBe(true);
    }
  });

  it("carries a frequency trace for every island that is not stable", () => {
    for (const island of CASCADE.islands) {
      if (island.state === "stable") continue;

      // No nadir means no trace, and that pairing is the invariant: a traced
      // island must have the number the trace bottoms out at.
      if (island.nadir === undefined) {
        expect(FREQ_TRACES.has(island.id), `island ${island.id} traced without a nadir`).toBe(false);
        continue;
      }

      const t = FREQ_TRACES.get(island.id);
      expect(t, `island ${island.id} has no trace`).toBeDefined();
      // Non-null: nadir is optional in the schema for an island with no
      // machine data, but every non-stable island in this fixture sets one.
      expect(Math.min(...(t ?? []))).toBeCloseTo(island.nadir!.value, 1);
    }
  });

  it("has a complete provenance chain on the inspected element", () => {
    expect(chainCompleteness(INSPECTED.element.provenance).complete).toBe(true);
  });

  it("includes backtests, with knowledge time well behind valid time", () => {
    const backtests = RUNS.filter((r) => r.scenario.startsWith("BACKTEST"));
    expect(backtests.length).toBeGreaterThan(0);
    for (const r of backtests) {
      expect(r.temporal.validTime).toBeDefined();
      expect(Date.parse(r.temporal.knowledgeTime)).toBeLessThan(
        Date.parse(r.temporal.validTime ?? ""),
      );
    }
  });

  it("includes validation runs with no model and no valid time", () => {
    const validation = RUNS.filter((r) => r.modelVersion === null);
    expect(validation.length).toBe(2);
    for (const r of validation) expect(r.temporal.validTime).toBeUndefined();
  });

  it("parses every run, including the incomplete ones", () => {
    expect(RUNS.every((r) => ScenarioRun.safeParse(r).success)).toBe(true);
    expect(RUNS.some((r) => r.status !== "complete")).toBe(true);
  });

  it("has a layer stack where every layer names its dataset version", () => {
    for (const l of LAYERS) expect(l.source.length).toBeGreaterThan(0);
  });
});
