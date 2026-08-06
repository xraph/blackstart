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
 * Reports whether a ring turns the same way at every vertex — the property a
 * fan triangulation needs to be exact (see `buildExtentGeometry`).
 *
 * Checked in a flat local projection (longitude scaled by the cosine of the
 * ring's mean latitude), the same approximation `shape.PolygonAreaM2` makes
 * server-side: adequate at corridor scale, and only the sign of each turn is
 * used here, so the projection's distortion cannot flip the answer.
 *
 * A closed ring — first point repeated last, as every `Corridor` is, and as
 * the wire therefore always sends — has that duplicate dropped first. Walking
 * triples over it directly does NOT work: both triples straddling the repeat
 * are degenerate, so the turn at vertex 0 is never evaluated, and vertex 0 is
 * the fan's apex. A concave ring whose only reflex vertex sat there would be
 * reported convex and fanned, filling the notch — the same over-claim the
 * convex hull made one package down, which is why that hull was removed.
 */
/**
 * Whether a ring repeats its first point last.
 *
 * Compared exactly rather than within a tolerance: the server closes a ring by
 * appending the same point, so the two are identical values, and a tolerance
 * here would also swallow a genuine short final segment.
 */
function closesOnItself(ring: readonly Geodetic[]): boolean {
  if (ring.length < 2) return false;

  const first = ring[0]!;
  const last = ring[ring.length - 1]!;

  return first.lon === last.lon && first.lat === last.lat;
}

function isConvexRing(ring: readonly Geodetic[]): boolean {
  const open = closesOnItself(ring) ? ring.slice(0, -1) : ring;

  if (open.length < 3) return true;

  const meanLat = open.reduce((sum, p) => sum + p.lat, 0) / open.length;
  const cosLat = Math.cos((meanLat * Math.PI) / 180);

  let sign = 0;

  for (let i = 0; i < open.length; i++) {
    const a = open[i]!;
    const b = open[(i + 1) % open.length]!;
    const c = open[(i + 2) % open.length]!;

    const abx = (b.lon - a.lon) * cosLat;
    const aby = b.lat - a.lat;
    const bcx = (c.lon - b.lon) * cosLat;
    const bcy = c.lat - b.lat;

    const cross = abx * bcy - aby * bcx;
    // Near-zero is a straight or degenerate turn — neither vertex nor its
    // sign says anything about convexity, so it is skipped rather than
    // treated as a sign of zero, which would falsely disagree with every
    // real turn.
    if (Math.abs(cross) < 1e-12) continue;

    const turn = Math.sign(cross);
    if (sign === 0) {
      sign = turn;
    } else if (turn !== sign) {
      return false;
    }
  }

  return true;
}

/**
 * The vertex and index buffers for a set of polygons, fan-triangulated ring
 * by ring.
 *
 * Fan triangulation — vertex 0 joined to every edge (i, i+1) — is exact for
 * a convex ring and wrong for a concave one, where it can emit a triangle
 * that covers ground outside the ring: the same over-claim `shape.Cover`'s
 * doc rejects a convex hull over several corridors for, one layer further
 * in. `shape.Corridor` is NOT guaranteed convex — it puts a reflex vertex at
 * every bend in the path it buffers, so a corridor that turns is concave at
 * that bend by construction. (An earlier version of this comment claimed
 * otherwise; it was wrong, and every span this package draws today happens
 * to be a straight two-point line — see `Line.Spans()` — which is why the
 * bug it describes has not yet been seen on screen.)
 *
 * Rather than build a general concave triangulator (ear clipping, ...),
 * this makes the constraint an enforced check: a ring that is not convex is
 * skipped — drawn as nothing — instead of fanned into triangles that would
 * claim ground its own corridor never covered. That is the same choice
 * `shape.Cover` makes about merging: a smaller result is preferable to a
 * dishonest one. A skipped ring is invisible on the globe rather than
 * reported, which is acceptable here because the input is server-computed
 * geometry, not user data whose loss would need surfacing — see this
 * function's tests for what a concave ring does instead of drawing wrong.
 *
 * Each surviving ring is triangulated into its own, disjoint slice of the
 * index buffer — no triangle's three vertices are ever drawn from two
 * different rings. A triangulation that reached across rings would fill the
 * ground between two unrelated extents with area nobody's corridor claims,
 * the same mistake a convex hull over several corridors makes one layer
 * down, just committed here instead.
 *
 * Separate from the `<Extent>` component so the geometry is testable without
 * a WebGL context — a polygon fanned from the wrong vertex still renders, it
 * renders the wrong shape, and that is worth asserting directly.
 */
export function buildExtentGeometry(
  area: readonly (readonly Geodetic[])[],
  lift: number,
): { positions: Float32Array; index: Uint32Array } {
  // Decided once, so the counting pass below and the building pass agree on
  // exactly the same set of rings.
  const keep = area.map((ring) => ring.length >= 3 && isConvexRing(ring));

  let vertexCount = 0;
  let triangleCount = 0;

  for (let i = 0; i < area.length; i++) {
    if (!keep[i]) continue;

    const ring = area[i]!;
    vertexCount += ring.length;
    triangleCount += ring.length - 2;
  }

  const positions = new Float32Array(vertexCount * 3);
  const index = new Uint32Array(triangleCount * 3);

  const scale = 1 + lift;
  let vertex = 0;
  let triangle = 0;

  for (let i = 0; i < area.length; i++) {
    if (!keep[i]) continue;

    const ring = area[i]!;
    const base = vertex;
    const ecef = toEcefArray(ring);

    for (let j = 0; j < ring.length; j++) {
      positions[vertex * 3] = (ecef[j * 3] ?? 0) * scale;
      positions[vertex * 3 + 1] = (ecef[j * 3 + 1] ?? 0) * scale;
      positions[vertex * 3 + 2] = (ecef[j * 3 + 2] ?? 0) * scale;
      vertex += 1;
    }

    // Fan from the ring's first vertex: (base, base+j+1, base+j+2) for every
    // interior edge. Indices are all within [base, base + ring.length), so
    // this ring's triangles can never reference another ring's vertices.
    for (let j = 0; j < ring.length - 2; j++) {
      index[triangle * 3] = base;
      index[triangle * 3 + 1] = base + j + 1;
      index[triangle * 3 + 2] = base + j + 2;
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
