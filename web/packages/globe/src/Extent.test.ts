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
});
