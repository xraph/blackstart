/**
 * Linear N-1 screening results.
 *
 * Distinct from {@link ContingencyList} in `risk.ts`, which they are easy to
 * confuse: both rank contingencies and they share almost nothing else. A
 * `Contingency` carries a probability and an expected cost and is what phase 6
 * will produce. A `ScreeningCandidate` is what the platform produces *today* —
 * a branch index, whether the outage could be screened at all, and the worst
 * loading the screen saw.
 *
 * Keeping them apart is what stops the console rendering one under the other's
 * header, which would require inventing the probabilities in between.
 */

import { z } from "zod";

/**
 * Whether the screen could evaluate an outage, and why not.
 *
 * The values are the platform's, from `extensions/contingency/sweep`. Not a
 * boolean, because "could not evaluate" has two causes that a reader treats
 * differently: a radial outage is unknown and consequential, and a branch that
 * was already out of service is neither.
 */
export const Screenability = z.enum(["screened", "radial", "not-applicable"]);
export type Screenability = z.infer<typeof Screenability>;

/** True only for outages the linear screen actually solved. */
export function evaluated(s: Screenability): boolean {
  return s === "screened";
}

/** One branch loaded past its rating under one outage. */
export const Violation = z.object({
  branch: z.number().int().nonnegative(),
  loading: z.number().nonnegative(),
  pFlow: z.number(),
  rating: z.number().positive(),
});
export type Violation = z.infer<typeof Violation>;

const ScreeningCandidateShape = z.object({
  contingencyId: z.string().min(1),
  /**
   * The branch index into the network model.
   *
   * An index and not a name because MATPOWER branches have no identifier of
   * their own — `extensions/networkmodel/matpower/parse.go` mints `branch-N`
   * from the position. Terse, and true; the branch-identity work is what
   * improves it, and manufacturing a name here would pre-empt it with a
   * fiction.
   */
  branch: z.number().int().nonnegative(),
  screenability: Screenability,
  /**
   * The highest loading the screen saw, absent where it could not look.
   *
   * Optional rather than defaulted to zero. A radial outage does not have a
   * loading of zero — it has no loading, and the difference is the entire
   * reason `screenability` exists. `extensions/contingency` makes the same
   * point about the same field: "That absence has to survive the crossing."
   */
  worstLoading: z.number().nonnegative().optional(),
  violations: z.array(Violation),
});

/**
 * A candidate, checked against the invariant the solver states.
 *
 * The proto is explicit: worst loading is "absent when screenability is not
 * SCREENED". A candidate carrying both a loading and a reason it could not be
 * evaluated is a contradiction, and it is the kind that renders — the row
 * appears, ranked by a number nothing measured.
 *
 * Only that direction is enforced. Nothing in the solver promises a screened
 * candidate always has a loading, and inventing the converse would reject
 * payloads the platform is entitled to send.
 */
export const ScreeningCandidate = ScreeningCandidateShape.superRefine((c, ctx) => {
  if (!evaluated(c.screenability) && c.worstLoading !== undefined) {
    ctx.addIssue({
      code: "custom",
      path: ["worstLoading"],
      message:
        `a ${c.screenability} contingency has no loading, but branch ${c.branch} ` +
        `carries ${c.worstLoading}`,
    });
  }
});
export type ScreeningCandidate = z.infer<typeof ScreeningCandidate>;

/**
 * A screening result, in whatever order it arrived.
 *
 * Deliberately carries **no ordering invariant**, unlike `ContingencyList`,
 * whose `superRefine` rejects an array whose expected cost climbs as you read
 * down. That invariant is why the console's sort control belongs to this type
 * and not to that one: re-sorting a `ContingencyList` produces a value that
 * fails its own validator.
 */
export const ScreeningList = z.array(ScreeningCandidate);
export type ScreeningList = z.infer<typeof ScreeningList>;

export const SCREENING_SORTS = ["loading", "violations", "branch"] as const;
export type ScreeningSort = (typeof SCREENING_SORTS)[number];

/**
 * Sort a screening result without losing the argument in its order.
 *
 * `extensions/contingency/sweep/sweep.go` puts unevaluated candidates ahead of
 * every evaluated one, because a radial outage that islands part of the
 * network has no loading to rank by and ranking it by the loading it does not
 * have buries the case that most needs simulating. So every key here sorts
 * *within* that bucket rather than across it — the reader chooses what orders
 * the evaluated ones, not whether the unknown ones stay visible.
 *
 * Ties break on branch index, which is canonical, so one ranking reads the
 * same way twice.
 */
export function sortScreening(rows: ScreeningList, key: ScreeningSort): ScreeningList {
  const bucket = (c: ScreeningCandidate): number => (evaluated(c.screenability) ? 1 : 0);

  const by = (a: ScreeningCandidate, b: ScreeningCandidate): number => {
    switch (key) {
      case "loading":
        /* Absent sorts below every present loading, including zero. The
         * schema keeps this off unevaluated candidates, so it is reached only
         * by a screened one the solver gave no loading for — and "we have no
         * number" should not outrank a measured 0.0. */
        return (b.worstLoading ?? -1) - (a.worstLoading ?? -1);
      case "violations":
        return b.violations.length - a.violations.length;
      case "branch":
        return 0;
    }
  };

  return [...rows].sort((a, b) => bucket(a) - bucket(b) || by(a, b) || a.branch - b.branch);
}
