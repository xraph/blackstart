/**
 * The vertex buffer a route becomes.
 *
 * `Routes` renders through React Three Fiber, which needs a WebGL context, so
 * what is tested here is the geometry it builds — extracted so the arithmetic
 * is checkable without a renderer. A polyline drawn with the wrong vertex
 * pairing still draws; it just draws the wrong shape, and no assertion about
 * "it rendered" catches that.
 */

import { describe, expect, it } from "vitest";
import { buildRouteSegments } from "./Routes";
import type { RoutePolyline } from "./Routes";

const line = (id: string, coords: readonly (readonly [number, number])[]): RoutePolyline => ({
  id,
  carrierId: `nmdl_${id}`,
  points: coords.map(([lon, lat]) => ({ lon, lat, elevation: 0 })),
});

describe("buildRouteSegments", () => {
  it("emits one segment per adjacent pair, not one per point", () => {
    // Four points is three segments. Off by one here draws a line back to the
    // origin, which reads as a stray conductor across the map.
    const { positions, segmentRoute } = buildRouteSegments(
      [line("a", [[-101, 34], [-101.2, 34.2], [-101.4, 34.4], [-101.6, 34.6]])],
      0,
    );

    expect(segmentRoute).toHaveLength(3);
    expect(positions).toHaveLength(3 * 6);
  });

  it("shares interior points between adjacent segments", () => {
    const { positions } = buildRouteSegments(
      [line("a", [[-101, 34], [-101.2, 34.2], [-101.4, 34.4]])],
      0,
    );

    // The end of segment 0 must be the start of segment 1, or the path has a
    // gap at every interior point.
    expect(Array.from(positions.slice(3, 6))).toEqual(Array.from(positions.slice(6, 9)));
  });

  it("maps every segment back to the route that owns it", () => {
    const { segmentRoute } = buildRouteSegments(
      [
        line("a", [[-101, 34], [-101.2, 34.2]]),
        line("b", [[-99, 32], [-99.2, 32.2], [-99.4, 32.4]]),
      ],
      0,
    );

    // One segment from a, two from b. A click reports a vertex index, and this
    // is what turns it back into a route.
    expect(Array.from(segmentRoute)).toEqual([0, 1, 1]);
  });

  it("skips a route with fewer than two points instead of emitting a degenerate segment", () => {
    const { positions, segmentRoute } = buildRouteSegments(
      [line("a", [[-101, 34]]), line("b", [[-99, 32], [-99.2, 32.2]])],
      0,
    );

    expect(segmentRoute).toHaveLength(1);
    expect(Array.from(segmentRoute)).toEqual([1]);
    expect(positions).toHaveLength(6);
  });

  it("lifts the path off the ellipsoid so it is not z-fought by the globe", () => {
    const flat = buildRouteSegments([line("a", [[0, 0], [1, 0]])], 0);
    const lifted = buildRouteSegments([line("a", [[0, 0], [1, 0]])], 0.01);

    const flatRadius = Math.hypot(flat.positions[0] ?? 0, flat.positions[1] ?? 0, flat.positions[2] ?? 0);
    const liftedRadius = Math.hypot(
      lifted.positions[0] ?? 0,
      lifted.positions[1] ?? 0,
      lifted.positions[2] ?? 0,
    );

    expect(liftedRadius).toBeGreaterThan(flatRadius);
    expect(liftedRadius / flatRadius).toBeCloseTo(1.01, 4);
  });

  it("produces an empty buffer for no routes rather than throwing", () => {
    const { positions, segmentRoute } = buildRouteSegments([], 0);

    expect(positions).toHaveLength(0);
    expect(segmentRoute).toHaveLength(0);
  });
});
