/**
 * Probabilities, the risk ramp, and ranked contingencies.
 *
 * Two design decisions from the console are encoded here as schema invariants
 * rather than left as documentation, because a rule the type system enforces
 * survives a refactor and a comment does not:
 *
 *  - a risk decomposition must actually add up to the probability it claims,
 *    or the waterfall drawn from it is a lie;
 *  - a contingency list must be ordered by expected cost, not by probability.
 *    That ordering *is* the project's coupling claim — the forecast decides
 *    which contingencies are likely and the network model decides what they
 *    cost, and the ranking is where those two meet.
 */

import { z } from "zod";
import { quantity } from "./quantity";
import { typeId } from "./identity";

/**
 * A probability in [0, 1].
 *
 * Branded so it cannot be confused with the percentages, per-unit loadings and
 * similarity scores that also live in the unit interval and mean different
 * things.
 */
export const Probability = z.number().min(0).max(1).brand<"Probability">();
export type Probability = z.infer<typeof Probability>;

export function probability(p: number): Probability {
  return Probability.parse(p);
}

/**
 * The class breaks the risk ramp is quantised to.
 *
 * These are display breaks, not model thresholds — five buckets is what a
 * person can distinguish along a line on a map. The action threshold is
 * separate and deliberately coincides with a break so that "above the third
 * band" and "actionable" are the same reading.
 */
export const RISK_BREAKS = [0.02, 0.05, 0.12, 0.25] as const;

/** Above this, a contingency is worth a human decision. */
export const ACTION_THRESHOLD = 0.05;

export type RiskBucket = 0 | 1 | 2 | 3 | 4;

export function riskBucket(p: number): RiskBucket {
  let bucket = 0;
  for (const brk of RISK_BREAKS) {
    if (p < brk) break;
    bucket += 1;
  }
  return bucket as RiskBucket;
}

/** Human labels for the buckets, used in legends and screen-reader text. */
export const RISK_BUCKET_LABELS: Record<RiskBucket, string> = {
  0: "negligible",
  1: "low",
  2: "elevated",
  3: "high",
  4: "severe",
};

/* ── decomposition ───────────────────────────────────────────────────── */

/**
 * One additive contribution to a probability.
 *
 * Additive rather than multiplicative because that is what the waterfall
 * draws, and a component whose visual is a stacked bar should not be fed
 * factors it has to log-transform to lay out.
 */
export const RiskContribution = z.object({
  feature: z.string().min(1),
  delta: z.number(),
  /** The raw feature value, so the bar can drill to what drove it. */
  evidence: z.string().min(1).optional(),
});
export type RiskContribution = z.infer<typeof RiskContribution>;

/** Contributions may miss the stated total by this much before it is a bug. */
const SUM_TOLERANCE = 5e-4;

export const RiskDecomposition = z
  .object({
    baseline: z.number().min(0).max(1),
    contributions: z.array(RiskContribution).min(1),
    total: Probability,
    /** The model that produced it, shown next to the section heading. */
    model: z.string().min(1),
  })
  .superRefine((d, ctx) => {
    const sum = d.contributions.reduce((acc, c) => acc + c.delta, d.baseline);
    if (Math.abs(sum - d.total) > SUM_TOLERANCE) {
      ctx.addIssue({
        code: "custom",
        path: ["total"],
        message:
          `decomposition does not reconcile: baseline plus contributions is ` +
          `${sum.toFixed(4)} but total is ${d.total.toFixed(4)}`,
      });
    }
  });
export type RiskDecomposition = z.infer<typeof RiskDecomposition>;

/**
 * A counterfactual, stated as one sentence an operator can act on.
 *
 * Deliberately not a feature-importance vector. Nobody pre-positions a crew
 * because of a SHAP value; they do it because the gust forecast is above a
 * number they recognise.
 */
export const Counterfactual = z.object({
  feature: z.string().min(1),
  current: quantity("m·s⁻¹").or(quantity("m")).or(quantity("%")).or(quantity("°C")),
  threshold: quantity("m·s⁻¹").or(quantity("m")).or(quantity("%")).or(quantity("°C")),
  /** The probability the element would carry at the threshold. */
  wouldFallTo: Probability,
  /** True when no single feature alone gets below the action threshold. */
  requiresMultiple: z.boolean(),
});
export type Counterfactual = z.infer<typeof Counterfactual>;

/** A past event resembling the present one, with what actually happened. */
export const HistoricalAnalogue = z.object({
  date: z.string().regex(/^\d{4}-\d{2}-\d{2}$/),
  similarity: z.number().min(0).max(1),
  event: z.string().min(1),
  outcome: z.string().min(1),
});
export type HistoricalAnalogue = z.infer<typeof HistoricalAnalogue>;

/* ── ranked contingencies ────────────────────────────────────────────── */

export const Contingency = z.object({
  rank: z.number().int().positive(),
  element: typeId("ln"),
  /** The name on the one-line diagram, not the identifier. */
  name: z.string().min(1),
  voltage: quantity("kV"),
  length: quantity("km"),
  probability: Probability,
  /** What losing it is expected to cost. This is the sort key. */
  expectedUnservedEnergy: quantity("MW·h"),
  /** Probability across the forecast horizon, for the row's sparkline. */
  horizon: z.array(Probability).min(2),
});
export type Contingency = z.infer<typeof Contingency>;

/**
 * A ranked list, validated against the ordering the console promises.
 *
 * Both invariants exist because the rail's whole argument is that rank 1 is
 * the thing most worth attention. A list whose ranks skip, or whose expected
 * cost climbs as you read down, breaks that promise silently — the rows still
 * render, they are just wrong.
 */
export const ContingencyList = z.array(Contingency).superRefine((rows, ctx) => {
  for (let i = 0; i < rows.length; i += 1) {
    const row = rows[i];
    if (row === undefined) continue;

    if (row.rank !== i + 1) {
      ctx.addIssue({
        code: "custom",
        path: [i, "rank"],
        message: `ranks must be contiguous from 1; row ${i} claims rank ${row.rank}`,
      });
    }

    const prev = i > 0 ? rows[i - 1] : undefined;
    if (
      prev !== undefined &&
      row.expectedUnservedEnergy.value > prev.expectedUnservedEnergy.value
    ) {
      ctx.addIssue({
        code: "custom",
        path: [i, "expectedUnservedEnergy"],
        message:
          `contingencies are ranked by expected cost, but row ${i} ` +
          `(${row.expectedUnservedEnergy.value} MW·h) exceeds row ${i - 1} ` +
          `(${prev.expectedUnservedEnergy.value} MW·h)`,
      });
    }
  }
});
export type ContingencyList = z.infer<typeof ContingencyList>;

/**
 * Sort into the order the schema requires.
 *
 * Offered so callers building a list from unordered results have a correct
 * path that does not involve guessing the invariant.
 */
export function rankByExpectedCost(rows: readonly Omit<Contingency, "rank">[]): Contingency[] {
  return [...rows]
    .sort((a, b) => b.expectedUnservedEnergy.value - a.expectedUnservedEnergy.value)
    .map((row, i) => ({ ...row, rank: i + 1 }));
}
