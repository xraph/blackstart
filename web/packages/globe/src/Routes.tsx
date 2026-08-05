import { useMemo } from "react";

import { toEcefArray, type Geodetic } from "./wgs84";

/**
 * One span of a conductor route: the stretch between two structures.
 *
 * The span is the unit rather than the route because everything measured
 * about a conductor is measured per span. A 200 km line passes through
 * several weather systems at once, ice is local, and a conductor breaks
 * between two structures — so a rating belongs to a span, and a renderer that
 * can only address a whole route cannot draw one.
 */
export interface RouteSpan {
  /** The span identifier, which hazard values are keyed by. */
  id: string;

  /** The path this span follows, in order. */
  points: readonly Geodetic[];
}

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

  /**
   * What this route carries, for cross-referencing the network: a branch in a
   * network model, or a circuit somebody actually built. The identifier's own
   * prefix says which — nmdl_ or circ_ — so there is no second field here to
   * disagree with it.
   */
  carrierId: string;

  /** The spans, in order along the conductor. */
  spans: readonly RouteSpan[];
}

/** Red, green, blue and alpha, each 0 to 1. */
export type Rgba = readonly [number, number, number, number];

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

  /**
   * The colour a span is drawn in.
   *
   * Omitted, every route draws in one colour and this package stays useful to
   * a viewer with no platform to ask for values.
   */
  colorOf?: ((spanId: string) => Rgba) | undefined;

  onSelect?: ((route: RoutePolyline) => void) | undefined;
}

const DEFAULT_LIFT = 0.0016;

/** Brighter than the schematic branches underneath, because where a route exists it is truer. */
const DEFAULT_COLOR = "#6ea8ff";

/**
 * Every span that is long enough to draw, paired with the route it belongs to.
 *
 * One pass, so the segment and colour buffers cannot disagree about which
 * spans are in the geometry — a colour buffer built from a different set of
 * spans than the positions is silently misaligned, and paints each span with
 * its neighbour's value.
 */
function drawableSpans(
  routes: readonly RoutePolyline[],
): { span: RouteSpan; routeIndex: number }[] {
  const out: { span: RouteSpan; routeIndex: number }[] = [];

  routes.forEach((route, routeIndex) => {
    for (const span of route.spans) {
      if (span.points.length >= 2) out.push({ span, routeIndex });
    }
  });

  return out;
}

/**
 * The vertex buffer for a set of routes, and the segment-to-owner indexes.
 *
 * Separate from the component so the arithmetic is testable without a WebGL
 * context. A polyline built with the wrong vertex pairing still renders — it
 * renders the wrong shape — so this is worth asserting directly rather than
 * through "it mounted".
 *
 * Segments are emitted within a span and never across two. A segment bridging
 * the end of one span and the start of the next belongs to neither, so no
 * value could colour it, and it would draw as an uncoloured gap through an
 * otherwise rated line.
 */
export function buildRouteSegments(
  routes: readonly RoutePolyline[],
  lift: number,
): { positions: Float32Array; segmentRoute: Int32Array; segmentSpan: string[] } {
  const spans = drawableSpans(routes);

  let segmentCount = 0;
  for (const { span } of spans) segmentCount += span.points.length - 1;

  const positions = new Float32Array(segmentCount * 6);
  const segmentRoute = new Int32Array(segmentCount);
  const segmentSpan: string[] = new Array<string>(segmentCount);

  let segment = 0;
  const scale = 1 + lift;

  for (const { span, routeIndex } of spans) {
    // Converted once per span rather than once per segment: every interior
    // point is shared by two segments, and converting twice would double the
    // geodetic maths for no benefit. Passed through rather than rebuilt —
    // copying the objects would re-declare an optional elevation as an
    // explicit undefined, which exactOptionalPropertyTypes rejects.
    const ecef = toEcefArray(span.points);

    for (let i = 0; i < span.points.length - 1; i++) {
      for (const end of [i, i + 1]) {
        const base = segment * 6 + (end === i ? 0 : 3);

        positions[base] = (ecef[end * 3] ?? 0) * scale;
        positions[base + 1] = (ecef[end * 3 + 1] ?? 0) * scale;
        positions[base + 2] = (ecef[end * 3 + 2] ?? 0) * scale;
      }

      segmentRoute[segment] = routeIndex;
      segmentSpan[segment] = span.id;
      segment += 1;
    }
  }

  return { positions, segmentRoute, segmentSpan };
}

/**
 * The per-vertex colour buffer, in the same order the positions are built.
 *
 * Four components rather than three. Colour carries the quantity being drawn
 * and alpha carries how much the value is to be trusted — a rating from a
 * surveyed conductor assignment and one assumed from the voltage class are
 * different claims, and three components would flatten them into one map.
 *
 * Both vertices of a segment take the same colour: a segment whose ends differ
 * is drawn as a gradient, which reads as a value changing along a span that
 * has exactly one.
 */
export function buildRouteColors(
  routes: readonly RoutePolyline[],
  colorOf: (spanId: string) => Rgba,
): Float32Array {
  const spans = drawableSpans(routes);

  let segmentCount = 0;
  for (const { span } of spans) segmentCount += span.points.length - 1;

  const colors = new Float32Array(segmentCount * 8);

  let segment = 0;

  for (const { span } of spans) {
    const [r, g, b, a] = colorOf(span.id);

    for (let i = 0; i < span.points.length - 1; i++) {
      for (const vertex of [0, 1]) {
        const base = segment * 8 + vertex * 4;

        colors[base] = r;
        colors[base + 1] = g;
        colors[base + 2] = b;
        colors[base + 3] = a;
      }

      segment += 1;
    }
  }

  return colors;
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
  colorOf,
  onSelect,
}: RoutesProps): React.JSX.Element | null {
  const { positions, segmentRoute } = useMemo(
    () => buildRouteSegments(routes, lift),
    [routes, lift],
  );

  const colors = useMemo(
    () => (colorOf === undefined ? null : buildRouteColors(routes, colorOf)),
    [routes, colorOf],
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
        {colors !== null && <bufferAttribute attach="attributes-color" args={[colors, 4]} />}
      </bufferGeometry>
      {colors === null ? (
        <lineBasicMaterial color={DEFAULT_COLOR} transparent opacity={0.9} />
      ) : (
        <lineBasicMaterial vertexColors transparent />
      )}
    </lineSegments>
  );
}
