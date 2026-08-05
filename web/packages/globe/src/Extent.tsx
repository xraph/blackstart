import { useMemo } from "react";
import * as THREE from "three";

import { toEcefArray, type Geodetic } from "./wgs84";

/**
 * How far below the routes the extent is drawn, as a fraction of the earth's
 * radius.
 *
 * Beneath `Routes`' own default lift (0.0016) and `Network`'s (0.00125), so a
 * conductor and the ground it covers never fight for the same pixel — the
 * extent is the backdrop, not another line competing with it.
 */
const DEFAULT_LIFT = 0.0008;

/** A translucent orange reads as "affected" without competing with the routes' blue. */
const DEFAULT_COLOR = "#ff8a3d";

/**
 * The vertex and index buffers for a set of polygons, fan-triangulated ring
 * by ring.
 *
 * Fan triangulation — vertex 0 joined to every edge (i, i+1) — is exact for
 * a convex ring and only wrong for a concave one where it can produce
 * triangles outside the ring. It is correct here: every ring this function
 * is fed is a `Corridor`, a buffer walked along a path and offset a fixed
 * distance to each side, which is convex at every straight run and turns
 * without folding back on itself at a bend. A general concave polygon
 * triangulator (ear clipping, ...) is more machinery than that shape needs.
 *
 * Each ring is triangulated into its own, disjoint slice of the index
 * buffer — no triangle's three vertices are ever drawn from two different
 * rings. A triangulation that reached across rings would fill the ground
 * between two unrelated extents with area nobody's corridor claims, the same
 * mistake a convex hull over several corridors makes one layer down (see
 * `shape.Cover`'s doc on the server), just committed here instead.
 *
 * Separate from the `<Extent>` component so the geometry is testable without
 * a WebGL context — a polygon fanned from the wrong vertex still renders, it
 * renders the wrong shape, and that is worth asserting directly.
 */
export function buildExtentGeometry(
  area: readonly (readonly Geodetic[])[],
  lift: number,
): { positions: Float32Array; index: Uint32Array } {
  let vertexCount = 0;
  let triangleCount = 0;

  for (const ring of area) {
    // A degenerate ring (fewer than 3 distinct points to fan from) has no
    // area and is skipped rather than emitted as zero-size triangles.
    if (ring.length < 3) continue;

    vertexCount += ring.length;
    triangleCount += ring.length - 2;
  }

  const positions = new Float32Array(vertexCount * 3);
  const index = new Uint32Array(triangleCount * 3);

  const scale = 1 + lift;
  let vertex = 0;
  let triangle = 0;

  for (const ring of area) {
    if (ring.length < 3) continue;

    const base = vertex;
    const ecef = toEcefArray(ring);

    for (let i = 0; i < ring.length; i++) {
      positions[vertex * 3] = (ecef[i * 3] ?? 0) * scale;
      positions[vertex * 3 + 1] = (ecef[i * 3 + 1] ?? 0) * scale;
      positions[vertex * 3 + 2] = (ecef[i * 3 + 2] ?? 0) * scale;
      vertex += 1;
    }

    // Fan from the ring's first vertex: (base, base+i+1, base+i+2) for every
    // interior edge. Indices are all within [base, base + ring.length), so
    // this ring's triangles can never reference another ring's vertices.
    for (let i = 0; i < ring.length - 2; i++) {
      index[triangle * 3] = base;
      index[triangle * 3 + 1] = base + i + 1;
      index[triangle * 3 + 2] = base + i + 2;
      triangle += 1;
    }
  }

  return { positions, index };
}

export interface ExtentProps {
  /** The ground covered, as closed rings — the wire shape `extentResponse.area` returns. */
  area: readonly (readonly Geodetic[])[];

  /** How far off the ellipsoid the surface is lifted. See `DEFAULT_LIFT`. */
  lift?: number;

  /** The fill colour. Solid rather than by-polygon: there is one kind of claim being drawn. */
  color?: string;
}

/**
 * The ground a cascade covered, filled rather than outlined.
 *
 * An outline reads as a boundary somebody surveyed. This is not that — it is
 * an area a simulation implied, and a filled, translucent surface is what
 * says so: it reads as coverage, not as a shape somebody drew a line around.
 */
export function Extent({
  area,
  lift = DEFAULT_LIFT,
  color = DEFAULT_COLOR,
}: ExtentProps): React.JSX.Element | null {
  const { positions, index } = useMemo(() => buildExtentGeometry(area, lift), [area, lift]);

  if (positions.length === 0) return null;

  return (
    <mesh>
      <bufferGeometry>
        <bufferAttribute attach="attributes-position" args={[positions, 3]} />
        <bufferAttribute attach="index" args={[index, 1]} />
      </bufferGeometry>
      <meshBasicMaterial
        color={color}
        transparent
        opacity={0.28}
        side={THREE.DoubleSide}
        depthWrite={false}
      />
    </mesh>
  );
}
