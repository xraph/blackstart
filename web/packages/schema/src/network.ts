/**
 * Network elements as the inspector shows them.
 *
 * This is the display projection, not the domain model — the platform's own
 * network model is far larger, and pulling all of it into the client would
 * make the inspector a schema browser. What is here is what an operator reads
 * when deciding whether a span is worth acting on.
 */

import { z } from "zod";
import { quantity } from "./quantity";
import { typeId, Alias } from "./identity";
import { cited } from "./provenance";
import { Probability, RiskDecomposition, Counterfactual, HistoricalAnalogue } from "./risk";

/** Voltage classes the console styles line weight by. */
export const VOLTAGE_CLASSES = [115, 230, 345, 500, 765] as const;

/**
 * Thermal ratings, which are temperature- and duration-dependent.
 *
 * Three of them rather than one because "the rating" is ambiguous — an element
 * at 118% of normal is fine for hours and an element at 118% of its short-term
 * emergency rating is about to trip, and a UI showing a single percentage
 * cannot tell those apart.
 */
export const ThermalRating = z.object({
  normal: quantity("A"),
  emergency: quantity("A"),
  shortTermEmergency: quantity("A").optional(),
  /** The ambient the ratings were computed at. */
  ambient: quantity("°C"),
});
export type ThermalRating = z.infer<typeof ThermalRating>;

/** A named series the inspector draws as a small sparkline. */
export const ConditionSeries = z.object({
  label: z.string().min(1),
  current: z.object({ value: z.number(), unit: z.string() }),
  /** Ascending over the forecast horizon; at least two points to draw. */
  points: z.array(z.number()).min(2),
  /** Which risk band the current value sits in, for the trace colour. */
  bucket: z.number().int().min(0).max(4),
});
export type ConditionSeries = z.infer<typeof ConditionSeries>;

export const TransmissionLine = z.object({
  ...cited.shape,
  id: typeId("ln"),
  name: z.string().min(1),
  voltage: quantity("kV"),
  length: quantity("km"),
  spanCount: z.number().int().positive(),
  structureCount: z.number().int().positive(),
  /** Conductor type as the asset record names it: `ACSR 954 kcmil "Rail"`. */
  conductor: z.string().min(1),
  bundle: z.number().int().positive(),
  rating: ThermalRating,
  /** Every external identifier that resolved here. Never emptied. */
  aliases: z.array(Alias),
});
export type TransmissionLine = z.infer<typeof TransmissionLine>;

/**
 * Everything the inspector panel needs for one element.
 *
 * Assembled as one payload rather than fetched piecewise because the panel is
 * useless partially filled: a risk figure with no provenance beside it is
 * exactly the thing the project's rules forbid displaying.
 */
export const ElementExplanation = z.object({
  element: TransmissionLine,
  probability: Probability,
  /** Where in the ranked list this element sits, for the header. */
  rank: z.number().int().positive().optional(),
  totalRanked: z.number().int().positive().optional(),
  decomposition: RiskDecomposition,
  conditions: z.array(ConditionSeries).min(1),
  analogues: z.array(HistoricalAnalogue),
  counterfactual: Counterfactual,
});
export type ElementExplanation = z.infer<typeof ElementExplanation>;
