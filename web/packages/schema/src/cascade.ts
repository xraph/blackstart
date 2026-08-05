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
import { cited, ProvenanceChain } from "./provenance";

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

/**
 * One sample off the solver's decimated frequency curve.
 *
 * `at` shares the cascade's own clock — the same seconds the events use — so a
 * trace can be plotted against the event log without a second time axis to
 * reconcile.
 */
export const FrequencySample = z.object({
  /** Seconds from the start of the cascade, the same clock the events use. */
  at: z.number().nonnegative().finite(),
  /** Absolute hertz. 60 Hz is 60, never a deviation and never per unit. */
  hz: z.number().finite(),
});
export type FrequencySample = z.infer<typeof FrequencySample>;

export const Island = z.object({
  id: IslandId,
  /** Generation minus load. Negative islands are shedding or collapsing. */
  balance: quantity("MW"),
  /**
   * Frequency at the end of the window shown.
   *
   * Absent, not zero: an island with no machine attached has no frequency to
   * report, and 0 Hz is a collapse reading, not a "no data" reading.
   */
  frequency: quantity("Hz").optional(),
  /** Lowest frequency reached, which is what decides whether it survives. Absent for the same reason as `frequency`. */
  nadir: quantity("Hz").optional(),
  state: z.enum(ISLAND_STATES),
  busCount: z.number().int().positive(),
  /**
   * The island's frequency curve, already decimated by the solver.
   *
   * Absent, not empty-as-zero: an island with no machine has no frequency to
   * plot, and a flat line at 0 Hz would render as a collapse that never
   * happened.
   */
  trace: z.array(FrequencySample).optional(),
});
export type Island = z.infer<typeof Island>;

export const CascadeRun = z
  .object({
    ...cited.shape,
    run: typeId("casc"),
    /** The outage that started it. */
    initiating: z.string().min(1),
    events: z.array(CascadeEvent).min(1),
    islands: z.array(Island).max(MAX_ISLANDS),
    /**
     * Absent, not zero: the engine has no customer model, so it never counts
     * who lost power. A zero here would assert nobody was affected, which is
     * a finding — and a calmer one than the truth, that nobody computed it.
     */
    customersInterrupted: quantity("customers").optional(),
    loadLost: quantity("MW"),
    /**
     * Absent for the same reason as `customersInterrupted`: this needs a
     * duration model over the outage the engine does not run.
     */
    expectedUnservedEnergy: quantity("MW·h").optional(),
    /**
     * Absent for the same reason as `customersInterrupted`: this needs the
     * geography the engine does not carry.
     */
    blastRadius: quantity("km²").optional(),
    /**
     * Absent for the same reason as `customersInterrupted`: this needs the
     * geography the engine does not carry.
     */
    countiesAffected: z.number().int().nonnegative().optional(),
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
 * The provenance a cascade run can honestly cite.
 *
 * Three links, from what the solver actually returns. There is no `dataset`
 * link because the solver was handed a model by content hash and never saw
 * the dataset behind it — so the chain is INCOMPLETE, which
 * `chainCompleteness` computes and the UI displays. Inventing a placeholder
 * dataset link to force completeness is the exact failure mode the
 * provenance module's own comment warns against.
 */
export function provenanceOf(input: {
  modelHash: string;
  solverVersion: string;
  run: string;
}): ProvenanceChain {
  return ProvenanceChain.parse([
    { kind: "network", label: "network model", ref: input.modelHash, digest: input.modelHash },
    { kind: "commit", label: "solver", ref: input.solverVersion },
    { kind: "run", label: "cascade run", ref: input.run },
  ]);
}

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
export const CASCADE_PHASES = ["trip", "redistribute", "separate", "shed", "settle"] as const;
export type CascadePhase = (typeof CASCADE_PHASES)[number];
