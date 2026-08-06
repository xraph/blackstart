import { describe, expect, it } from "vitest";
import { buildExtentGeometry } from "./Extent";

const ring = (coords: readonly (readonly [number, number])[]) =>
  coords.map(([lon, lat]) => ({ lon, lat, elevation: 0 }));

describe("buildExtentGeometry", () => {
  it("triangulates a ring into a fillable surface", () => {
    // A four-point ring is two triangles. Emitting the ring as a line strip
    // would draw an outline, and an outline of a blast radius reads as a
    // boundary somebody surveyed rather than as an area.
    const { index } = buildExtentGeometry(
      [
        ring([
          [-101, 34],
          [-100, 34],
          [-100, 33],
          [-101, 33],
        ]),
      ],
      0,
    );

    expect(index).toHaveLength(2 * 3);
  });

  it("keeps disjoint polygons separate", () => {
    // Two extents must not share an edge. A triangulation that spanned them
    // would fill the ground between two failures nobody reported.
    const { index } = buildExtentGeometry(
      [
        ring([
          [-101, 34],
          [-100, 34],
          [-100, 33],
        ]),
        ring([
          [-97, 30],
          [-96, 30],
          [-96, 29],
        ]),
      ],
      0,
    );

    expect(index).toHaveLength(2 * 3);
  });

  it("produces an empty buffer for no area rather than throwing", () => {
    const { positions, index } = buildExtentGeometry([], 0);

    expect(positions).toHaveLength(0);
    expect(index).toHaveLength(0);
  });

  // THE assertion for this module. A fan from vertex 0 of a concave ring
  // draws a triangle that covers ground outside the ring — here, the notch
  // of an L-bend, exactly the over-claim `shape.Cover`'s doc rejects for a
  // convex hull over two corridors, one layer further in. `shape.Corridor`
  // puts a reflex vertex at every bend, so this is not a hypothetical input:
  // it is what a corridor around a path with more than two points produces.
  it("skips a concave ring rather than fanning it into an over-claiming triangle", () => {
    // An L-shaped hexagon, reflex at (1, 1): the notch between the two arms
    // (around (1.5, 1.5)) is outside the ring, but a fan from (0, 0) would
    // draw a triangle straight across it.
    const lShape = ring([
      [0, 0],
      [2, 0],
      [2, 1],
      [1, 1],
      [1, 2],
      [0, 2],
    ]);

    const { positions, index } = buildExtentGeometry([lShape], 0);

    expect(index).toHaveLength(0);
    expect(positions).toHaveLength(0);
  });

  it("still triangulates a convex ring alongside a concave one it skips", () => {
    const lShape = ring([
      [0, 0],
      [2, 0],
      [2, 1],
      [1, 1],
      [1, 2],
      [0, 2],
    ]);
    const rectangle = ring([
      [-101, 34],
      [-100, 34],
      [-100, 33],
      [-101, 33],
    ]);

    const { index } = buildExtentGeometry([lShape, rectangle], 0);

    // Only the rectangle contributes: two triangles, six indices.
    expect(index).toHaveLength(2 * 3);
  });
});

describe("a concave ring is never fan-triangulated", () => {
  /**
   * An L, wound so its one reflex vertex sits at index 0.
   *
   * That position is the whole point. A closed ring repeats its first point
   * last, and the two triples straddling that duplicate are both degenerate —
   * so a convexity check that walks triples without dropping the duplicate
   * never evaluates the turn at vertex 0, which is also the fan's apex.
   */
  const L_REFLEX_AT_ZERO: readonly (readonly [number, number])[] = [
    [-101 + 0.01, 34 + 0.01],
    [-101 + 0.01, 34 + 0.02],
    [-101 + 0.0, 34 + 0.02],
    [-101 + 0.0, 34 + 0.0],
    [-101 + 0.02, 34 + 0.0],
    [-101 + 0.02, 34 + 0.01],
  ];

  it("skips it when the ring is open", () => {
    const { index } = buildExtentGeometry([ring(L_REFLEX_AT_ZERO)], 0);

    expect(index).toHaveLength(0);
  });

  it("skips it when the ring is closed, which is what the wire sends", () => {
    // `shape.Corridor` closes every ring it produces, so this is the shape
    // that actually arrives. A check that only works on open rings would
    // triangulate this one and fill the concave notch — the same over-claim
    // the convex hull made at the geometry layer, one package down.
    const closed = [...L_REFLEX_AT_ZERO, L_REFLEX_AT_ZERO[0]!];

    const { index } = buildExtentGeometry([ring(closed)], 0);

    expect(index).toHaveLength(0);
  });
});
