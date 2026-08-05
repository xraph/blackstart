/**
 * The joins between a hazard value and a span on the map.
 *
 * Three of these encode facts about the platform that are not guessable from
 * its types, and each was found by asking a running host: issuance comes from
 * the last computation rather than the last ingested cycle, a value is keyed
 * by an instant rather than a string, and ampacity runs the opposite way from
 * loading.
 */

import { describe, expect, it } from "vitest";
import type { SpanValue, valuesResponse } from "@blackstart/api-client";

import {
  AMPACITY,
  LOADING,
  basisByRoute,
  colorOf,
  computationFor,
  rampIndex,
  valueIndex,
} from "./hazard";

const computation = (id: string, computedAt: string, cycleId = `wxcyc_${id}`) => ({
  id: `hzcmp_${id}`,
  cycleId,
  runId: `run_${id}`,
  computedAt,
  spans: 21,
  unrated: 917,
  values: 735,
});

const value = (spanId: string, quantity: string, validAt: string, v: number): SpanValue => ({
  spanId,
  quantity,
  issuedAt: "2026-08-04T03:00:00Z",
  validAt,
  value: v,
});

const response = (spanId: string, values: SpanValue[]): valuesResponse => ({
  spanId,
  issuedAt: "2026-08-04T03:00:00Z",
  values,
});

describe("computationFor", () => {
  const PASSES = [
    computation("c", "2026-08-04T14:51:01Z"),
    computation("b", "2026-08-04T09:20:00Z"),
    computation("a", "2026-08-04T04:10:00Z"),
  ];

  it("takes the newest pass when no knowledge time is pinned", () => {
    expect(computationFor(PASSES)?.id).toBe("hzcmp_c");
  });

  it("takes the newest pass regardless of the order they arrive in", () => {
    // The platform returns these newest first, but a layer that silently
    // depends on that draws yesterday's ratings the day the order changes.
    const shuffled = [PASSES[1], PASSES[2], PASSES[0]].filter((c) => c !== undefined);

    expect(computationFor(shuffled)?.id).toBe("hzcmp_c");
  });

  it("takes the newest pass at or before a pinned knowledge time", () => {
    // This is what makes an as-of view mean what it says: the console pinned
    // to 10:00 must show what the system knew then, not what it knows now.
    expect(computationFor(PASSES, "2026-08-04T10:00:00Z")?.id).toBe("hzcmp_b");
  });

  it("finds nothing when every pass ran after the pinned time", () => {
    // Not the oldest pass as a fallback — the system knew nothing yet, and
    // showing the earliest ratings would be a leak from the future.
    expect(computationFor(PASSES, "2026-08-04T01:00:00Z")).toBeNull();
  });

  it("finds nothing when no pass has run at all", () => {
    expect(computationFor([])).toBeNull();
  });
});

describe("basisByRoute", () => {
  const ASSIGNMENTS = [
    { id: "hzasg_1", carrierId: "nmdl_h27", conductorCode: "drake", basis: "surveyed" as const },
    {
      id: "hzasg_2",
      carrierId: "nmdl_k92",
      conductorCode: "hawk",
      basis: "assumed_by_voltage" as const,
    },
  ];

  it("keys the basis by the route the branch turned out to have", () => {
    // The join that makes the layer work at all. Ratings live on a branch,
    // the map draws routes, and a branch route is not the branch — looking
    // span ids up against the routes the console happens to draw first
    // colours nothing and reads as a rendering fault.
    const routes = [{ id: "rte_a" }, { id: "rte_b" }];

    expect(basisByRoute(ASSIGNMENTS, routes)).toEqual(
      new Map([
        ["rte_a", "surveyed"],
        ["rte_b", "assumed_by_voltage"],
      ]),
    );
  });

  it("skips an assignment whose branch has no route yet", () => {
    // A branch with a conductor but no traced geometry is rateable and not
    // drawable. Dropping it beats pairing it with somebody else's route.
    const routes = [null, { id: "rte_b" }];

    expect(basisByRoute(ASSIGNMENTS, routes)).toEqual(new Map([["rte_b", "assumed_by_voltage"]]));
  });
});

describe("valueIndex", () => {
  const RESPONSES = [
    response("span_1", [
      value("span_1", "ampacity", "2026-08-04T03:00:00Z", 1437.8),
      value("span_1", "ampacity", "2026-08-04T04:00:00Z", 1551.2),
    ]),
    response("span_2", [value("span_2", "ampacity", "2026-08-04T04:00:00Z", 980.4)]),
  ];

  it("picks the value valid at the hour being shown", () => {
    const index = valueIndex(RESPONSES, "ampacity", "2026-08-04T04:00:00Z");

    expect(index.get("span_1")).toBeCloseTo(1551.2, 1);
    expect(index.get("span_2")).toBeCloseTo(980.4, 1);
  });

  it("matches on the instant rather than on the text", () => {
    // The console carries "2026-08-04T04:00Z" and the platform returns
    // "2026-08-04T04:00:00Z". Comparing strings finds nothing and draws an
    // empty layer over data that is present.
    const index = valueIndex(RESPONSES, "ampacity", "2026-08-04T04:00Z");

    expect(index.get("span_1")).toBeCloseTo(1551.2, 1);
  });

  it("omits a span rated at other hours but not at this one", () => {
    // Distinct from a span that was never rated, and the panel says which.
    const index = valueIndex(RESPONSES, "ampacity", "2026-08-04T03:00:00Z");

    expect(index.has("span_1")).toBe(true);
    expect(index.has("span_2")).toBe(false);
  });

  it("keeps the quantities apart", () => {
    const mixed = [
      response("span_1", [
        value("span_1", "ampacity", "2026-08-04T03:00:00Z", 1437.8),
        value("span_1", "combined_load", "2026-08-04T03:00:00Z", 0.45),
      ]),
    ];

    expect(valueIndex(mixed, "combined_load", "2026-08-04T03:00:00Z").get("span_1")).toBeCloseTo(
      0.45,
      2,
    );
  });
});

describe("rampIndex", () => {
  it("runs low-is-severe for a rating, because a derated line is the bad case", () => {
    // Ampacity is headroom. A span whose limit collapses in the heat must not
    // draw green, which is what a naive high-is-severe ramp would do.
    expect(rampIndex(400, AMPACITY)).toBe(4);
    expect(rampIndex(1600, AMPACITY)).toBe(0);
  });

  it("runs high-is-severe for loading, because ice and wind accumulate", () => {
    expect(rampIndex(0.4, LOADING)).toBe(0);
    expect(rampIndex(40, LOADING)).toBe(4);
  });

  it("puts a value exactly on a break in the higher bucket", () => {
    const [first] = LOADING.breaks;

    expect(rampIndex(first ?? 0, LOADING)).toBe(1);
  });
});

describe("colorOf", () => {
  it("draws a surveyed rating at full strength", () => {
    expect(colorOf(1600, "surveyed", AMPACITY)[3]).toBe(1);
  });

  it("draws an assumed rating faintly, because it is a weaker claim", () => {
    // Same colour, less certainty. Spending the colour channel on the basis
    // would leave nothing to carry the quantity.
    const surveyed = colorOf(1600, "surveyed", AMPACITY);
    const assumed = colorOf(1600, "assumed_by_voltage", AMPACITY);

    expect(assumed.slice(0, 3)).toEqual(surveyed.slice(0, 3));
    expect(assumed[3]).toBeLessThan(surveyed[3]);
  });

  it("draws an unrated span neutral rather than at the safe end of the ramp", () => {
    // The failure that matters: an unrated span drawn green says the line is
    // fine, when what is true is that nobody has computed anything about it.
    const unrated = colorOf(null, null, AMPACITY);
    const safe = colorOf(1600, "surveyed", AMPACITY);

    expect(unrated).not.toEqual(safe);
    expect(unrated[0]).toBeCloseTo(unrated[1], 2);
    expect(unrated[1]).toBeCloseTo(unrated[2], 2);
  });
});
