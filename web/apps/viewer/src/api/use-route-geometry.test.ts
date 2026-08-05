/**
 * Assembling spans into a path.
 *
 * The failure this guards against renders without erroring: spans taken in
 * arrival order rather than sequence order draw a star burst from wherever the
 * first span happened to be, which reads as a renderer bug and is a data bug.
 */

import { describe, expect, it } from "vitest";
import type { Span } from "@blackstart/api-client";
import { polylineOfSpans } from "./use-route-geometry";

const span = (sequence: number, coords: readonly (readonly [number, number])[]): Span =>
  ({
    id: `span_${sequence}`,
    routeId: "rt_1",
    sequence,
    lengthM: 1000,
    azimuthDeg: 0,
    geometry: coords.map(([lon, lat]) => ({ lon, lat, elevation: 0 })),
  }) as Span;

describe("polylineOfSpans", () => {
  it("orders by sequence, not by arrival", () => {
    const scrambled = [
      span(2, [
        [-101.2, 34.2],
        [-101.3, 34.3],
      ]),
      span(0, [
        [-101.0, 34.0],
        [-101.1, 34.1],
      ]),
      span(1, [
        [-101.1, 34.1],
        [-101.2, 34.2],
      ]),
    ];

    const line = polylineOfSpans("rt_1", "br_1", scrambled);

    expect(line).not.toBeNull();
    expect(line?.points.map((p) => Number(p.lon.toFixed(1)))).toEqual([
      -101.0, -101.1, -101.1, -101.2, -101.2, -101.3,
    ]);
  });

  it("returns null for a route with too few points to be a line", () => {
    expect(polylineOfSpans("rt_1", "nmdl_1", [])).toBeNull();
    expect(polylineOfSpans("rt_1", "nmdl_1", [span(0, [[-101, 34]])])).toBeNull();
  });

  it("keeps every point, including the duplicates where spans meet", () => {
    // Adjacent spans share a structure, so the joining point appears twice.
    // Deduplicating would be tidier and would also silently shorten the path
    // if two spans genuinely met at different coordinates.
    const line = polylineOfSpans("rt_1", "nmdl_1", [
      span(0, [
        [-101.0, 34.0],
        [-101.1, 34.1],
      ]),
      span(1, [
        [-101.1, 34.1],
        [-101.2, 34.2],
      ]),
    ]);

    expect(line?.points).toHaveLength(4);
  });

  it("carries the identifiers a click needs to report", () => {
    const line = polylineOfSpans("rt_9", "nmdl_9", [
      span(0, [
        [-101, 34],
        [-101.1, 34.1],
      ]),
    ]);

    expect(line?.id).toBe("rt_9");
    expect(line?.carrierId).toBe("nmdl_9");
  });

  // A route may carry a circuit rather than a branch, and the polyline has to
  // pass that through untouched. The prefix is the only thing saying which,
  // so a helper that normalised or validated it here would be a second place
  // for the two to disagree.
  it("passes a circuit carrier through unchanged", () => {
    const line = polylineOfSpans("rt_3", "circ_3", [
      span(0, [
        [-97.8, 30.2],
        [-97.7, 30.3],
      ]),
    ]);

    expect(line?.carrierId).toBe("circ_3");
  });
});
