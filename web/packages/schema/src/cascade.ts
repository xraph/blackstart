/**
 * Cascade runs: the event sequence, the islands it produced, and the totals.
 *
 * The playback surface is driven entirely by this shape. Two invariants are
 * enforced because breaking either produces a plausible-looking animation of
 * something that did not happen: events must be ordered in simulated time, and
 * an event may only reference an island the run actually formed.
 */

import { z } from "zod";
import { quantity } from "./quantity";
import { typeId } from "./identity";
import { cited } from "./provenance";

/**
 * What happened, in the order a cascade produces it.
 *
 * `overload` precedes `trip` because an element that trips on overload spends
 * time above its rating first, and the log shows both — an operator reading
 * back a cascade wants to know how long the margin was gone before protection
 * acted.
 */
export const CASCADE_EVENT_KINDS = ["overload", "trip", "island", "shed", "stabilise"] as const;
export type CascadeEventKind = (typeof CASCADE_EVENT_KINDS)[number];

/** The most islands the display can colour apart. */
export const MAX_ISLANDS = 8;

export const IslandId = z.number().int().min(1).max(MAX_ISLANDS);
export type IslandId = z.infer<typeof IslandId>;

export const CascadeEvent = z.object({
  /** Seconds since the initiating outage. */
  t: z.number().nonnegative().finite(),
  kind: z.enum(CASCADE_EVENT_KINDS),
  /** The element as an operator names it: `ln TULIA–PLAINVIEW 345`. */
  element: z.string().min(1),
  /** Why: `dist zone 1`, `163% · 2 s`, `underfreq`. */
  detail: z.string().min(1),
  island: IslandId.optional(),
});
export type CascadeEvent = z.infer<typeof CascadeEvent>;

export const ISLAND_STATES = ["stable", "recovering", "shedding", "collapsed"] as const;
export type IslandState = (typeof ISLAND_STATES)[number];

export const Island = z.object({
  id: IslandId,
  /** Generation minus load. Negative islands are shedding or collapsing. */
  balance: quantity("MW"),
  /** Frequency at the end of the window shown. */
  frequency: quantity("Hz"),
  /** Lowest frequency reached, which is what decides whether it survives. */
  nadir: quantity("Hz"),
  state: z.enum(ISLAND_STATES),
  busCount: z.number().int().positive(),
});
export type Island = z.infer<typeof Island>;

export const CascadeRun = z
  .object({
    ...cited.shape,
    run: typeId("run"),
    /** The outage that started it. */
    initiating: z.string().min(1),
    events: z.array(CascadeEvent).min(1),
    islands: z.array(Island).max(MAX_ISLANDS),
    customersInterrupted: quantity("customers"),
    loadLost: quantity("MW"),
    expectedUnservedEnergy: quantity("MW·h"),
    blastRadius: quantity("km²"),
    countiesAffected: z.number().int().nonnegative(),
  })
  .superRefine((run, ctx) => {
    for (let i = 1; i < run.events.length; i += 1) {
      const prev = run.events[i - 1];
      const cur = run.events[i];
      if (prev !== undefined && cur !== undefined && cur.t < prev.t) {
        ctx.addIssue({
          code: "custom",
          path: ["events", i, "t"],
          message: `events must be ordered in simulated time; ${cur.t}s follows ${prev.t}s`,
        });
      }
    }

    const formed = new Set(run.islands.map((i) => i.id));
    run.events.forEach((e, i) => {
      if (e.island !== undefined && !formed.has(e.island)) {
        ctx.addIssue({
          code: "custom",
          path: ["events", i, "island"],
          message: `event references island ${e.island}, which this run never formed`,
        });
      }
    });
  });
export type CascadeRun = z.infer<typeof CascadeRun>;

/**
 * Events newest-first, which is how the log reads.
 *
 * The stored order is ascending because that is the order the simulator emits
 * and the order the invariant checks; reversing is a display concern.
 */
export function newestFirst(events: readonly CascadeEvent[]): CascadeEvent[] {
  return [...events].reverse();
}

/**
 * Positions along the event-density track, in [0, 1].
 *
 * The scrubber is keyed on event time rather than linear time because cascades
 * are bursty: a linear axis spends most of its width on the seconds where
 * nothing happens, which is exactly the part nobody needs to scrub through.
 */
export function eventDensity(events: readonly CascadeEvent[]): number[] {
  const last = events[events.length - 1];
  const span = last === undefined || last.t === 0 ? 1 : last.t;
  return events.map((e) => e.t / span);
}

/** The five phases the transport labels, as fractions of the run. */
export const CASCADE_PHASES = [
  "trip",
  "redistribute",
  "separate",
  "shed",
  "settle",
] as const;
export type CascadePhase = (typeof CASCADE_PHASES)[number];
