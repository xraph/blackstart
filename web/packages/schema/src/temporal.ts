/**
 * The two time axes.
 *
 * Valid time is when a fact was true of the physical world. Knowledge time is
 * when the system learned it. Keeping them apart is the single most important
 * modelling decision in the project and the most common source of silent
 * leakage in outage-prediction work, so the client refuses to let them be
 * interchanged: they are separately branded, and a function that wants a
 * knowledge time will not accept a valid time even though both are ISO strings.
 *
 * Every instant is UTC with a trailing Z. A grid spans time zones and a naive
 * local timestamp in a forecast pipeline is a bug waiting for a DST boundary.
 */

import { z } from "zod";

const UTC_INSTANT = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d{1,9})?)?Z$/;

const utcInstant = z
  .string()
  .regex(UTC_INSTANT, "instants must be UTC in ISO-8601 form with a trailing Z")
  .refine((s) => !Number.isNaN(Date.parse(s)), "not a real instant");

/** When the fact was true of the world. */
export const ValidTime = utcInstant.brand<"ValidTime">();
export type ValidTime = z.infer<typeof ValidTime>;

/** When the system learned it. */
export const KnowledgeTime = utcInstant.brand<"KnowledgeTime">();
export type KnowledgeTime = z.infer<typeof KnowledgeTime>;

/**
 * A bitemporal reference — the pair that every result carries.
 *
 * There is deliberately no single "timestamp" field anywhere in this package.
 * Offering one would be offering the mistake.
 */
export const BitemporalRef = z.object({
  validTime: ValidTime,
  knowledgeTime: KnowledgeTime,
});
export type BitemporalRef = z.infer<typeof BitemporalRef>;

/**
 * Whether the view is following the present or pinned to a past knowledge
 * time.
 *
 * The interface treats these differently: an as-of view outlines the frame in
 * amber, because a number computed from a deliberately-stale knowledge state
 * looks exactly like a live one otherwise.
 */
export type TemporalMode = "live" | "as-of";

/**
 * Classify a reference against the wall clock.
 *
 * `toleranceMs` exists because "live" in practice means "the most recent
 * knowledge time the pipeline has published", which trails now by however long
 * the last ingest took. Ninety minutes covers an hourly forecast cycle plus
 * its processing without calling a routine view a backtest.
 */
export function temporalMode(
  ref: BitemporalRef,
  now: Date = new Date(),
  toleranceMs = 90 * 60 * 1000,
): TemporalMode {
  const known = Date.parse(ref.knowledgeTime);
  return now.getTime() - known <= toleranceMs ? "live" : "as-of";
}

/**
 * True when the reference describes a backtest: the system is being asked what
 * it would have said at a knowledge time long before the forecast it is
 * evaluating.
 */
export function isBacktest(ref: BitemporalRef, now: Date = new Date()): boolean {
  return temporalMode(ref, now) === "as-of";
}

/**
 * Reject a reference that leaks the future into a backtest.
 *
 * A model evaluated as of some past moment must not see a record corrected a
 * week later. Any payload whose knowledge time postdates its valid time by
 * more than the forecast lead is either a live forecast or a leak, and the two
 * are distinguishable only by intent — so this is offered as an explicit check
 * rather than folded into the schema.
 */
export function leaksFuture(ref: BitemporalRef, maxLeadMs = 0): boolean {
  return Date.parse(ref.knowledgeTime) - Date.parse(ref.validTime) > maxLeadMs;
}

/** Formats an instant the way the console prints it: `2026-08-02 22:00Z`. */
export function formatInstant(t: string): string {
  const m = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2})/.exec(t);
  return m === null ? t : `${m[1]} ${m[2]}Z`;
}

/**
 * A forecast horizon: the cycle it came from and the lead hours it covers.
 *
 * The time scrubber is bound to this rather than to arbitrary wall time,
 * because a forecast only exists at the hours its cycle produced.
 */
export const ForecastHorizon = z.object({
  /** The model run that produced it, e.g. `HRRR 2026-08-02 18Z`. */
  cycle: z.string().min(1),
  issued: KnowledgeTime,
  /** Lead hours available, ascending and contiguous from the cycle time. */
  leadHours: z.array(z.number().int().nonnegative()).min(2),
});
export type ForecastHorizon = z.infer<typeof ForecastHorizon>;
