import { useMemo } from "react";

import { toEcefArray, type Geodetic } from "./wgs84";

/**
 * One conductor route, as the surveyed or inferred path its spans follow.
 *
 * A polyline rather than a pair of endpoints. A branch drawn straight between
 * its buses is a schematic; a 30 km line crosses a ridge, turns at an angle
 * point, and sags between structures, and the difference is exactly what
 * decides whether a span is exposed to a gust or sheltered from it. Drawing
 * the real path is not decoration — it is the geometry the weather is sampled
 * along.
 */
export interface RoutePolyline {
  /** The route identifier, reported on selection. */
  id: string;

  /** The branch this route belongs to, for cross-referencing the network. */
  branchId: string;

  /**
   * The path, in order. Longitude, latitude and elevation per point, matching
   * the order the platform's geometry is stored and served in.
   */
  points: readonly Geodetic[];
}

export interface RoutesProps {
  routes: readonly RoutePolyline[];

  /**
   * How far off the ellipsoid the path is lifted, as a fraction of the earth's
   * radius.
   *
   * Routes sit slightly above the surface so they are not z-fought by the
   * globe mesh they lie on. Slightly higher than the network's own lift, so a
   * route and the straight branch it replaces do not fight each other either.
   */
  lift?: number;

  onSelect?: ((route: RoutePolyline) => void) | undefined;
}

const DEFAULT_LIFT = 0.0016;

/**
 * The vertex buffer for a set of routes, and the segment-to-route index.
 *
 * Separate from the component so the arithmetic is testable without a WebGL
 * context. A polyline built with the wrong vertex pairing still renders — it
 * renders the wrong shape — so this is worth asserting directly rather than
 * through "it mounted".
 */
export function buildRouteSegments(
  routes: readonly RoutePolyline[],
  lift: number,
): { positions: Float32Array; segmentRoute: Int32Array } {
  let segmentCount = 0;

  for (const route of routes) {
    if (route.points.length >= 2) segmentCount += route.points.length - 1;
  }

  const positions = new Float32Array(segmentCount * 6);
  const segmentRoute = new Int32Array(segmentCount);

  let segment = 0;
  const scale = 1 + lift;

  routes.forEach((route, routeIndex) => {
    if (route.points.length < 2) return;

    // Converted once per route rather than once per segment: every interior
    // point is shared by two segments, and converting twice would double the
    // geodetic maths for no benefit. Passed through rather than rebuilt —
    // copying the objects would re-declare an optional elevation as an
    // explicit undefined, which exactOptionalPropertyTypes rejects.
    const ecef = toEcefArray(route.points);

    for (let i = 0; i < route.points.length - 1; i++) {
      for (const end of [i, i + 1]) {
        const base = segment * 6 + (end === i ? 0 : 3);

        positions[base] = (ecef[end * 3] ?? 0) * scale;
        positions[base + 1] = (ecef[end * 3 + 1] ?? 0) * scale;
        positions[base + 2] = (ecef[end * 3 + 2] ?? 0) * scale;
      }

      segmentRoute[segment] = routeIndex;
      segment += 1;
    }
  });

  return { positions, segmentRoute };
}

/**
 * Conductor routes, drawn as connected segments.
 *
 * Emitted as `lineSegments` rather than one `line` per route: a few hundred
 * routes is a few hundred draw calls that way, and one buffer is one. The cost
 * is that a click reports the segment index, which is mapped back to its route
 * through the index built alongside the vertices.
 */
export function Routes({
  routes,
  lift = DEFAULT_LIFT,
  onSelect,
}: RoutesProps): React.JSX.Element | null {
  const { positions, segmentRoute } = useMemo(
    () => buildRouteSegments(routes, lift),
    [routes, lift],
  );

  if (positions.length === 0) return null;

  return (
    <lineSegments
      onPointerDown={(event) => {
        event.stopPropagation();

        const vertex = event.index;
        if (vertex === undefined) return;

        const route = routes[segmentRoute[Math.floor(vertex / 2)] ?? -1];
        if (route !== undefined) onSelect?.(route);
      }}
    >
      <bufferGeometry>
        <bufferAttribute attach="attributes-position" args={[positions, 3]} />
      </bufferGeometry>
      {/* Brighter than the schematic branches underneath, because where a
          route exists it is the more truthful of the two. */}
      <lineBasicMaterial color="#6ea8ff" transparent opacity={0.9} />
    </lineSegments>
  );
}
