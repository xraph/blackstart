/**
 * The URL contract.
 *
 * Every screen reads its state from here rather than from component state,
 * because a view that cannot be linked to cannot be cited. Given that no
 * result is displayed in this client without a provenance link, a URL that
 * reproduces a screenshot is part of that requirement rather than a routing
 * nicety — a reviewer needs to arrive at the same numbers, not a similar view.
 *
 * Knowledge time is optional and its absence *is* the live case. Encoding
 * "live" as a missing parameter rather than a sentinel keeps the ordinary URL
 * short and makes a backtest visibly different in the address bar, which is
 * the one place nobody can style away.
 */

import { z } from "zod";
import {
  AnyTypeId,
  KnowledgeTime,
  ValidTime,
  typeId,
  type BitemporalRef,
} from "@blackstart/schema";

/** Lead hours an HRRR cycle publishes; the scrubber cannot leave them. */
export const MAX_LEAD_HOUR = 18;

export const consoleSearch = z.object({
  scenario: z.string().min(1).default("tx-2026-0802-derecho"),
  valid: ValidTime,
  /** Absent means live. Present means the console is pinned to a past view. */
  known: KnowledgeTime.optional(),
  /**
   * Coerced because a search param arrives as a string. Bounded because a
   * forecast only exists at the hours its cycle produced, so a lead outside
   * the horizon is not a clamp-able mistake — it is a request for a fact that
   * does not exist.
   */
  lead: z.coerce.number().int().min(0).max(MAX_LEAD_HOUR).default(4),
  /**
   * Which hazard quantity is drawn over the network.
   *
   * In the URL because it is part of what a screenshot shows, and a figure in
   * a review has to be reproducible from its link. Rejected rather than
   * defaulted when it names something this build cannot draw: quietly falling
   * back to the rating answers a question nobody asked, under a link that says
   * otherwise.
   */
  layer: z.enum(["ampacity", "loading"]).default("ampacity"),

  /** The selected element, which is what opens the inspector. */
  sel: AnyTypeId.optional(),
});
export type ConsoleSearch = z.infer<typeof consoleSearch>;

export const DEFAULT_CONSOLE_SEARCH = {
  scenario: "tx-2026-0802-derecho",
  lead: 4,
} as const;

/**
 * The bitemporal reference a URL describes.
 *
 * With no knowledge time the system is being asked what it knows now, which
 * for display purposes is the valid time itself. Returning a complete pair
 * either way means no component downstream has to handle a half-populated
 * reference, which is where the two axes would get conflated.
 */
export function temporalOf(search: ConsoleSearch): BitemporalRef {
  return {
    validTime: search.valid,
    knowledgeTime: (search.known ?? search.valid) as BitemporalRef["knowledgeTime"],
  };
}

/**
 * The cascade screen's URL contract.
 *
 * `run` and not the playhead: the playhead moves many times a second and would
 * flood the history stack, while the run is the thing worth sharing a link to.
 * The screen's own comment has said this since before there was an API to
 * fetch one from.
 *
 * Optional because arriving with no run is a legitimate state — the screen
 * says so and points at the runs index rather than guessing which run to show.
 */
export const cascadeSearch = consoleSearch.extend({
  run: typeId("casc").optional(),
});
export type CascadeSearch = z.infer<typeof cascadeSearch>;

export const runsSearch = z.object({
  kind: z.enum(["all", "cascade", "sweep", "validation"]).default("all"),
  status: z.enum(["any", "complete", "running", "failed", "queued"]).default("any"),
  /**
   * Runs selected for comparison. Capped at two because the exit from this
   * screen is a difference surface, and a difference is between two things —
   * a third selection is a mistake worth rejecting rather than silently
   * dropping.
   */
  compare: z.array(z.string()).max(2).default([]),
});
export type RunsSearch = z.infer<typeof runsSearch>;
