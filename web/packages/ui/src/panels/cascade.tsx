/**
 * Island legend and cascade totals.
 *
 * The counters are large tabular figures rather than gauges. A dial would
 * round away the digits an operator is actually reading — "1 284 610
 * customers" and "about 1.3 million" are not the same statement in a report.
 */

import type { ReactElement } from "react";
import type { CascadeRun, Island } from "@blackstart/schema";
import { cn } from "../cn";
import { islandBg, islandVar } from "../lib/palette";
import { QuantityBlock, QuantityValue } from "../instrument/quantity";
import { FrequencyTrace } from "../instrument/sparkline";

const STATE_NOTE: Record<Island["state"], string> = {
  stable: "stable",
  recovering: "recovering",
  shedding: "shedding",
  collapsed: "collapsed",
};

export function IslandLegend({ islands }: { readonly islands: readonly Island[] }): ReactElement {
  return (
    <div>
      {islands.map((i) => (
        <div
          key={i.id}
          className="flex h-[30px] items-center gap-2 border-b border-line-2 px-[9px] last:border-b-0"
        >
          <i aria-hidden="true" className={cn("size-[9px] shrink-0 rounded-[2px]", islandBg(i.id))} />
          <span className="w-14 shrink-0 text-[11px] text-fg">Island {i.id}</span>
          <QuantityValue q={i.balance} unitless className="w-[52px] shrink-0 text-[9.5px] text-fg-3" />
          <span className="flex-1 text-right font-mono text-[9.5px] tabular text-fg-3">
            <QuantityValue q={i.frequency} unitless className="text-[9.5px]" /> Hz
          </span>
          <span className="sr-only">{STATE_NOTE[i.state]}</span>
        </div>
      ))}
    </div>
  );
}

export function CascadeCounters({
  run,
  traces,
}: {
  readonly run: CascadeRun;
  /** Per-island frequency samples, keyed by island id. */
  readonly traces: ReadonlyMap<number, readonly number[]>;
}): ReactElement {
  return (
    <div className="flex h-[92px] shrink-0 items-stretch border-t border-line bg-panel">
      <QuantityBlock
        label="Customers interrupted"
        q={run.customersInterrupted}
        note="restoration horizon 8 h"
        className="min-w-[184px] border-r border-line"
      />
      <QuantityBlock
        label="Load lost"
        q={run.loadLost}
        className="min-w-[184px] border-r border-line"
      />
      <QuantityBlock
        label="Expected unserved energy"
        q={run.expectedUnservedEnergy}
        className="min-w-[184px] border-r border-line"
      />

      <div className="flex flex-1 gap-4 overflow-hidden py-2 pl-[18px] pr-3.5">
        {run.islands
          .filter((i) => traces.has(i.id))
          .map((i) => (
            <div key={i.id} className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5 font-mono text-[9px] tabular text-fg-3">
                <i aria-hidden="true" className={cn("size-1.5 shrink-0 rounded-[1px]", islandBg(i.id))} />
                Island {i.id}{" "}
                <span className="text-fg-2">
                  nadir <QuantityValue q={i.nadir} unitless className="text-[9px]" /> Hz
                </span>
              </div>
              <FrequencyTrace points={traces.get(i.id) ?? []} colour={islandVar(i.id)} />
            </div>
          ))}
      </div>
    </div>
  );
}
