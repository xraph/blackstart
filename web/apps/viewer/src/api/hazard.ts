/**
 * Turning hazard values into something the globe can draw.
 *
 * The joins here encode three facts about the platform that its types do not
 * carry, each established against a running host:
 *
 *   - Issuance comes from the last *computation*, not the last ingested cycle.
 *     The newest cycle is `fetched` and answers every rating request with
 *     nothing; the newest cycle a pass actually ran against is older. Reading
 *     issuance from `weather/cycles` draws an empty layer over data that is
 *     perfectly fine.
 *   - A value is keyed by an instant, not by a string. The console carries
 *     `2026-08-04T04:00Z` and the platform returns `2026-08-04T04:00:00Z`.
 *   - Ampacity runs the opposite way from loading. A rating is headroom, so a
 *     small one is the severe case; a load is a burden, so a large one is.
 */

import type { Computation, SpanValue, valuesResponse } from "@blackstart/api-client";
import type { Rgba } from "@blackstart/globe";

/** How a conductor came to be assigned to a branch, as `hazard` records it. */
export type Basis = "surveyed" | "assumed_by_voltage";

/**
 * A drawable quantity, with the class breaks the legend prints.
 *
 * The breaks are stated rather than derived from the data in view. A ramp
 * rescaled to whatever happens to be on screen makes two screenshots
 * incomparable, and the console's legend exists to print real breaks.
 */
export interface Encoding {
  /** The layer id, and what the URL would carry. */
  readonly id: string;
  readonly label: string;
  /** The quantity name on the wire. */
  readonly quantity: string;
  readonly unit: string;
  /** Four ascending breaks, giving five buckets. */
  readonly breaks: readonly [number, number, number, number];
  /** Which end of the scale is the bad one. */
  readonly severity: "high" | "low";
}

/**
 * Thermal rating, in amperes.
 *
 * Breaks bracket what an ACSR conductor of the sizes this platform carries
 * actually rates at — the observed range for Drake under a summer forecast sits
 * in the top bucket, and a span falling through them is one losing headroom.
 */
export const AMPACITY: Encoding = {
  id: "ampacity",
  label: "Ampacity",
  quantity: "ampacity",
  unit: "A",
  breaks: [800, 1000, 1200, 1400],
  severity: "low",
};

/** Mechanical loading: the resultant of wind drag and ice weight, N/m. */
export const LOADING: Encoding = {
  id: "loading",
  label: "Loading",
  quantity: "combined_load",
  unit: "N/m",
  breaks: [2, 5, 10, 20],
  severity: "high",
};

/**
 * The ramp, as the canvas needs it.
 *
 * Literal rather than read from `--bs-risk-*`. A WebGL buffer cannot take a
 * token, and resolving one through `getComputedStyle` on every frame would
 * make the renderer depend on the stylesheet having loaded. These mirror the
 * dark-ground token values, which is the ground the stage is always on.
 */
const RAMP: readonly Rgba[] = [
  [0.176, 0.416, 0.31, 1], // #2d6a4f
  [0.416, 0.6, 0.333, 1], // #6a9955
  [0.788, 0.635, 0.153, 1], // #c9a227
  [0.878, 0.478, 0.184, 1], // #e07a2f
  [0.757, 0.071, 0.122, 1], // #c1121f
];

/**
 * What an unrated span is drawn in.
 *
 * Deliberately not the safe end of the ramp. A span with no conductor
 * assignment drawn green says the line is fine; what is true is that nobody
 * has computed anything about it, and those are not the same claim.
 */
const UNRATED: Rgba = [0.42, 0.42, 0.42, 0.35];

/** How faint a rating conditioned on an assumed conductor is drawn. */
const ASSUMED_ALPHA = 0.45;

/**
 * The pass whose ratings should be shown.
 *
 * Sorted rather than trusting arrival order: the platform returns these newest
 * first, but a layer that silently depends on that draws yesterday's ratings
 * the day the ordering changes.
 *
 * With a knowledge time pinned, the newest pass at or before it — and nothing
 * at all if every pass ran later. Falling back to the oldest would be a leak
 * from the future dressed as a default.
 */
export function computationFor(
  computations: readonly Computation[],
  knownAt?: string | undefined,
): Computation | null {
  const limit = knownAt === undefined ? Infinity : Date.parse(knownAt);

  const eligible = computations
    .filter((c) => Date.parse(c.computedAt) <= limit)
    .sort((a, b) => Date.parse(b.computedAt) - Date.parse(a.computedAt));

  return eligible[0] ?? null;
}

/**
 * Route to conductor basis, for the routes that can rate at all.
 *
 * The join the whole layer rests on. A rating belongs to a *branch* — that is
 * what carries a conductor assignment — while the map draws routes, and the
 * two are not the same thing: nine of the routes on this platform carry OSM
 * circuits and the rated one carries an ACTIVSg branch. Looking span ids up
 * against the routes the console happens to draw first colours nothing and
 * reads as a rendering fault rather than as a missing join.
 *
 * `routes[i]` is the route resolved for `assignments[i]`. A branch with a
 * conductor but no traced geometry is rateable and not drawable, so it is
 * dropped rather than paired with somebody else's route.
 */
export function basisByRoute(
  assignments: readonly { carrierId: string; basis: string }[],
  routes: readonly ({ id: string } | null | undefined)[],
): Map<string, Basis> {
  const out = new Map<string, Basis>();

  assignments.forEach((assignment, index) => {
    const route = routes[index];
    if (route === null || route === undefined) return;

    out.set(route.id, assignment.basis as Basis);
  });

  return out;
}

/**
 * Span to value, for one quantity at one forecast hour.
 *
 * A span absent from the result is a span with no value *at this hour*, which
 * the panel reports differently from one that was never rated at all.
 */
export function valueIndex(
  responses: readonly valuesResponse[],
  quantity: string,
  validAt: string,
): Map<string, number> {
  const instant = Date.parse(validAt);
  const index = new Map<string, number>();

  for (const response of responses) {
    for (const v of response.values as SpanValue[]) {
      if (v.quantity !== quantity) continue;
      if (Date.parse(v.validAt) !== instant) continue;

      index.set(v.spanId, v.value);
    }
  }

  return index;
}

/**
 * Which of the five buckets a value falls in, 0 being the benign end.
 *
 * A value exactly on a break belongs to the higher bucket, so a span at the
 * threshold is reported as having crossed it rather than as having not.
 */
export function rampIndex(value: number, encoding: Encoding): number {
  const crossed = encoding.breaks.filter((b) => value >= b).length;

  return encoding.severity === "high" ? crossed : encoding.breaks.length - crossed;
}

/**
 * The colour a span is drawn in.
 *
 * Colour carries the quantity and alpha carries the basis. A rating from a
 * surveyed conductor assignment and one assumed from the voltage class are
 * different claims — the second is indicative where the first is
 * authoritative — and spending the colour channel on that distinction would
 * leave nothing to carry the measurement.
 */
export function colorOf(value: number | null, basis: Basis | null, encoding: Encoding): Rgba {
  if (value === null) return UNRATED;

  const [r, g, b] = RAMP[rampIndex(value, encoding)] ?? RAMP[0]!;

  return [r, g, b, basis === "assumed_by_voltage" ? ASSUMED_ALPHA : 1];
}
