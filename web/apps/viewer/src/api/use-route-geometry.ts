/**
 * Conductor routes and the span geometry that makes them a path.
 *
 * Two round trips by necessity: the platform lists routes in one call and
 * serves each route's spans in another. There is no bulk geometry endpoint,
 * so this fans out — bounded, because a continental network is thousands of
 * routes and issuing thousands of requests to draw a first frame is not a
 * loading strategy.
 *
 * The cap is reported rather than applied silently. A map showing 200 of 2000
 * routes looks exactly like a map of a 200-route network, and the difference
 * matters to anyone reading it.
 */

import { useMemo } from "react";
import { useQueries } from "@tanstack/react-query";
import { queryKeys, useGeoRoutesList, type Span } from "@blackstart/api-client";
import type { RoutePolyline } from "@blackstart/globe";

import { useApiClient } from "./provider";

/** How many routes' geometry is fetched for a first view. */
export const DEFAULT_ROUTE_LIMIT = 200;

export interface RouteGeometry {
  readonly routes: readonly RoutePolyline[];

  /** True while the route list or any span request is outstanding. */
  readonly isPending: boolean;

  /** The first failure, if the list or any span request failed. */
  readonly error: Error | null;

  /** How many routes the platform reported, before the cap. */
  readonly available: number;

  /** How many were actually fetched. Less than `available` means truncated. */
  readonly fetched: number;
}

/**
 * Assembles one route's spans into a path.
 *
 * Ordered by the sequence the platform assigns rather than by arrival order.
 * A path is only a path if its points run in the order the conductor does, and
 * a route assembled out of order draws as a star burst rather than a line —
 * which looks like a rendering bug and is a data-ordering one.
 *
 * Spans are kept as spans rather than flattened into one point list. The span
 * id is the only key a hazard value has, so flattening loses the ability to
 * colour a rating at all — and the flattened form also manufactured a segment
 * between the last point of one span and the first of the next, which belongs
 * to neither.
 *
 * Returns null for a route with no span long enough to be a line, rather than
 * a degenerate polyline the renderer would silently skip.
 */
export function polylineOfSpans(
  id: string,
  carrierId: string,
  spans: readonly Span[],
): RoutePolyline | null {
  const drawable = [...spans]
    .sort((a, b) => a.sequence - b.sequence)
    .filter((span) => span.geometry.length >= 2)
    .map((span) => ({
      id: span.id,
      points: span.geometry.map((p) => ({ lon: p.lon, lat: p.lat, elevation: p.elevation })),
    }));

  if (drawable.length === 0) return null;

  return { id, carrierId, spans: drawable };
}

export function useRouteGeometry(limit: number = DEFAULT_ROUTE_LIMIT): RouteGeometry {
  const client = useApiClient();
  const list = useGeoRoutesList(client, limit);

  const routes = useMemo(() => list.data?.routes ?? [], [list.data]);

  /*
   * useQueries rather than a loop over the generated hook: a hook cannot be
   * called a variable number of times. The keys come from the generated
   * builders anyway, so a span fetched here and one fetched by
   * useGeoRoutesSpans elsewhere share a cache entry rather than each holding
   * their own copy.
   */
  const spans = useQueries({
    queries: routes.map((route) => ({
      queryKey: queryKeys.geoRoutesSpans(route.id),
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        client.geo.routes.spans(route.id, { signal }),
    })),
  });

  return useMemo(() => {
    const polylines: RoutePolyline[] = [];

    for (const [index, route] of routes.entries()) {
      const result = spans[index];
      const data = result?.data;

      if (data === undefined) continue;

      const polyline = polylineOfSpans(route.id, route.carrierId, data.spans);
      if (polyline !== null) polylines.push(polyline);
    }

    const failure =
      list.error ?? (spans.find((s) => s.error !== null)?.error as Error | undefined) ?? null;

    return {
      routes: polylines,
      isPending: list.isPending || spans.some((s) => s.isPending),
      error: failure,
      available: routes.length,
      fetched: polylines.length,
    };
  }, [routes, spans, list.error, list.isPending]);
}
