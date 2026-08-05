/**
 * The hazard values behind the layer, fetched the cheap way.
 *
 * Assignment-driven rather than span-driven. `hazard/assignments` names the
 * carriers that have a conductor, and a span on a carrier without one is
 * *known* unrated without being asked — so this fetches values for the spans
 * that can have them and nothing else. On the platform this was written
 * against that is one assignment, twenty-one spans and twenty-three requests,
 * where fanning out over every drawn span would have been three hundred and
 * forty to find the same twenty-one.
 *
 * The whole forecast window is fetched once rather than the hour in view, so
 * scrubbing the time axis is free. The values are indexed locally.
 */

import { useMemo } from "react";
import { useQueries } from "@tanstack/react-query";
import {
  queryKeys,
  useHazardAssignmentsList,
  useHazardComputationsList,
  useWeatherCyclesGet,
} from "@blackstart/api-client";
import type { RoutePolyline } from "@blackstart/globe";

import { useApiClient } from "./provider";
import { basisByRoute, computationFor, valueIndex, type Basis, type Encoding } from "./hazard";

/** How far past issuance the values are fetched, in hours. */
const HORIZON_HOURS = 18;

export interface HazardLayer {
  /** The value and basis for a span, or null when it has neither. */
  readonly lookup: (spanId: string) => { value: number; basis: Basis } | null;

  /** Spans carrying a value at the hour in view. */
  readonly rated: number;

  /** Spans drawn with no value at that hour. */
  readonly unrated: number;

  /** The cycle issuance the values were computed from, or null if no pass has run. */
  readonly issuance: string | null;

  /** The pass itself, for the legend to name. */
  readonly computationId: string | null;

  readonly isPending: boolean;
  readonly error: Error | null;
}

function plusHours(instant: string, hours: number): string {
  return new Date(Date.parse(instant) + hours * 3_600_000).toISOString();
}

export function useHazardLayer(
  routes: readonly RoutePolyline[],
  encoding: Encoding,
  validAt: string,
  knownAt?: string | undefined,
): HazardLayer {
  const client = useApiClient();

  const passes = useHazardComputationsList(client);
  const assignments = useHazardAssignmentsList(client);

  /* Issuance comes from the last computation, not the last ingested cycle.
   * The newest cycle is `fetched` and answers every rating request with
   * nothing; the newest cycle a pass actually ran against is older. */
  const pass = useMemo(
    () => computationFor(passes.data?.computations ?? [], knownAt),
    [passes.data, knownAt],
  );

  const cycle = useWeatherCyclesGet(client, pass?.cycleId ?? "", {
    enabled: pass !== null,
  });

  const issuance = cycle.data?.issuedAt ?? null;

  /* A rating belongs to a carrier and the map draws routes, so the carrier has
   * to be resolved to one. One request per assignment, which is the smallest
   * this join can be. */
  const carriers = useMemo(() => assignments.data?.assignments ?? [], [assignments.data]);

  const carrierRoutes = useQueries({
    queries: carriers.map((a) => ({
      queryKey: queryKeys.geoBranchesRoute(a.carrierId),
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        client.geo.branches.route(a.carrierId, { signal }),
    })),
  });

  const basisOf = useMemo(
    () =>
      basisByRoute(
        carriers,
        carrierRoutes.map((r) => r.data ?? null),
      ),
    [carriers, carrierRoutes],
  );

  /* Only the spans that can carry a value. Everything else is unrated as a
   * fact about the platform rather than as a request that came back empty. */
  const rateable = useMemo(
    () =>
      routes
        .filter((route) => basisOf.has(route.id))
        .flatMap((route) =>
          route.spans.map((span) => ({ spanId: span.id, basis: basisOf.get(route.id)! })),
        ),
    [routes, basisOf],
  );

  const series = useQueries({
    queries: rateable.map(({ spanId }) => ({
      queryKey: [encoding.id, spanId, issuance] as const,
      queryFn: ({ signal }: { signal: AbortSignal }) => {
        const from = issuance ?? "";
        const to = plusHours(from, HORIZON_HOURS);

        return encoding.id === "loading"
          ? client.hazard.spans.loading(spanId, from, from, to, { signal })
          : client.hazard.spans.rating(spanId, from, from, to, { signal });
      },
      enabled: issuance !== null,
    })),
  });

  return useMemo(() => {
    const values = valueIndex(
      series.flatMap((s) => (s.data === undefined ? [] : [s.data])),
      encoding.quantity,
      validAt,
    );

    const basisBySpan = new Map(rateable.map(({ spanId, basis }) => [spanId, basis]));

    const lookup = (spanId: string): { value: number; basis: Basis } | null => {
      const value = values.get(spanId);
      const basis = basisBySpan.get(spanId);

      if (value === undefined || basis === undefined) return null;

      return { value, basis };
    };

    const drawn = routes.reduce((n, route) => n + route.spans.length, 0);

    const failure =
      passes.error ??
      assignments.error ??
      (cycle.error as Error | null) ??
      (carrierRoutes.find((r) => r.error !== null)?.error as Error | undefined) ??
      (series.find((s) => s.error !== null)?.error as Error | undefined) ??
      null;

    return {
      lookup,
      rated: values.size,
      unrated: drawn - values.size,
      issuance,
      computationId: pass?.id ?? null,
      isPending:
        passes.isPending ||
        assignments.isPending ||
        carrierRoutes.some((r) => r.isPending) ||
        series.some((s) => s.isPending),
      error: failure,
    };
  }, [
    series,
    rateable,
    routes,
    encoding.quantity,
    validAt,
    issuance,
    pass,
    passes.error,
    passes.isPending,
    assignments.error,
    assignments.isPending,
    cycle.error,
    carrierRoutes,
  ]);
}
