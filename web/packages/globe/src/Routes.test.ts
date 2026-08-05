/**
 * The vertex buffer a route becomes.
 *
 * `Routes` renders through React Three Fiber, which needs a WebGL context, so
 * what is tested here is the geometry it builds — extracted so the arithmetic
 * is checkable without a renderer. A polyline drawn with the wrong vertex
 * pairing still draws; it just draws the wrong shape, and no assertion about
 * "it rendered" catches that.
 *
 * The span boundary is the case worth watching. A route is a sequence of
 * spans, and a segment that bridges two of them belongs to neither — which is
 * invisible while every route is one colour and wrong the moment a span
 * carries its own.
 */

import { describe, expect, it } from "vitest";
import { buildRouteColors, buildRouteSegments } from "./Routes";
import type { RoutePolyline } from "./Routes";

/** A route from spans, each given as its own list of coordinates. */
const line = (
  id: string,
  spans: readonly (readonly (readonly [number, number])[])[],
): RoutePolyline => ({
  id,
  carrierId: `nmdl_${id}`,
  spans: spans.map((coords, index) => ({
    id: `span_${id}_${index}`,
    points: coords.map(([lon, lat]) => ({ lon, lat, elevation: 0 })),
  })),
});

describe("buildRouteSegments", () => {
  it("emits one segment per adjacent pair, not one per point", () => {
    // Four points is three segments. Off by one here draws a line back to the
    // origin, which reads as a stray conductor across the map.
    const { positions, segmentRoute } = buildRouteSegments(
      [
        line("a", [
          [
            [-101, 34],
            [-101.2, 34.2],
            [-101.4, 34.4],
            [-101.6, 34.6],
          ],
        ]),
      ],
      0,
    );

    expect(segmentRoute).toHaveLength(3);
    expect(positions).toHaveLength(3 * 6);
  });

  it("never joins the end of one span to the start of the next", () => {
    // Two spans of two points each is two segments. A third would bridge them,
    // and that segment belongs to neither span — so it could not be coloured
    // by a rating, and would draw as an uncoloured gap through a rated line.
    const { segmentRoute, segmentSpan } = buildRouteSegments(
      [
        line("a", [
          [
            [-101, 34],
            [-101.2, 34.2],
          ],
          [
            [-101.2, 34.2],
            [-101.4, 34.4],
          ],
        ]),
      ],
      0,
    );

    expect(segmentRoute).toHaveLength(2);
    expect(Array.from(segmentSpan)).toEqual(["span_a_0", "span_a_1"]);
  });

  it("shares interior points between adjacent segments of one span", () => {
    const { positions } = buildRouteSegments(
      [
        line("a", [
          [
            [-101, 34],
            [-101.2, 34.2],
            [-101.4, 34.4],
          ],
        ]),
      ],
      0,
    );

    // The end of segment 0 must be the start of segment 1, or the path has a
    // gap at every interior point.
    expect(Array.from(positions.slice(3, 6))).toEqual(Array.from(positions.slice(6, 9)));
  });

  it("maps every segment back to the route that owns it", () => {
    const { segmentRoute } = buildRouteSegments(
      [
        line("a", [
          [
            [-101, 34],
            [-101.2, 34.2],
          ],
        ]),
        line("b", [
          [
            [-99, 32],
            [-99.2, 32.2],
            [-99.4, 32.4],
          ],
        ]),
      ],
      0,
    );

    // One segment from a, two from b. A click reports a vertex index, and this
    // is what turns it back into a route.
    expect(Array.from(segmentRoute)).toEqual([0, 1, 1]);
  });

  it("skips a span too short to be a line without dropping the rest of its route", () => {
    const { segmentSpan } = buildRouteSegments(
      [
        line("a", [
          [[-101, 34]],
          [
            [-101.2, 34.2],
            [-101.4, 34.4],
          ],
        ]),
      ],
      0,
    );

    expect(Array.from(segmentSpan)).toEqual(["span_a_1"]);
  });

  it("lifts the path off the ellipsoid so it is not z-fought by the globe", () => {
    const at = (lift: number) =>
      buildRouteSegments(
        [
          line("a", [
            [
              [0, 0],
              [1, 0],
            ],
          ]),
        ],
        lift,
      );

    const radius = (p: Float32Array): number => Math.hypot(p[0] ?? 0, p[1] ?? 0, p[2] ?? 0);

    expect(radius(at(0.01).positions) / radius(at(0).positions)).toBeCloseTo(1.01, 4);
  });

  it("produces an empty buffer for no routes rather than throwing", () => {
    const { positions, segmentRoute, segmentSpan } = buildRouteSegments([], 0);

    expect(positions).toHaveLength(0);
    expect(segmentRoute).toHaveLength(0);
    expect(segmentSpan).toHaveLength(0);
  });
});

describe("buildRouteColors", () => {
  const ROUTE = line("a", [
    [
      [-101, 34],
      [-101.2, 34.2],
    ],
    [
      [-101.2, 34.2],
      [-101.4, 34.4],
    ],
  ]);

  it("gives both ends of a segment the same colour", () => {
    // A segment whose ends differ is drawn as a gradient, which reads as a
    // value changing along a span that carries one value.
    const colors = buildRouteColors([ROUTE], () => [0.9, 0.2, 0.1, 1]);

    expect(Array.from(colors.slice(0, 4))).toEqual(Array.from(colors.slice(4, 8)));
  });

  it("colours each span by its own identifier", () => {
    const colors = buildRouteColors([ROUTE], (spanId) =>
      spanId === "span_a_0" ? [1, 0, 0, 1] : [0, 0, 1, 0.5],
    );

    expect(Array.from(colors.slice(0, 4))).toEqual([1, 0, 0, 1]);
    expect(Array.from(colors.slice(8, 12))).toEqual([0, 0, 1, 0.5]);
  });

  it("writes four components per vertex so alpha can carry the basis", () => {
    // Two segments, two vertices each, RGBA. Three components here would drop
    // alpha silently and flatten a surveyed rating onto an assumed one.
    const colors = buildRouteColors([ROUTE], () => [1, 1, 1, 1]);

    expect(colors).toHaveLength(2 * 2 * 4);
  });
});
