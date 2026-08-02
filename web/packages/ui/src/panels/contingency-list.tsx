/**
 * The ranked contingency rail.
 *
 * Sorted by expected cost, not by probability — that ordering is the project's
 * coupling claim surfacing in the interface, and the schema enforces it, so
 * this component can render `rows` in the order given without re-sorting.
 */

import type { ReactElement } from "react";
import type { Contingency, ContingencyList } from "@blackstart/schema";
import { cn } from "../cn";
import { QuantityValue } from "../instrument/quantity";
import { ProbabilityValue, RiskBar } from "../instrument/risk";
import { Sparkline } from "../instrument/sparkline";

export function ContingencyRail({
  rows,
  selected,
  onSelect,
}: {
  readonly rows: ContingencyList;
  readonly selected?: string;
  readonly onSelect?: (c: Contingency) => void;
}): ReactElement {
  /* One ceiling across every sparkline, so rows are visually comparable. */
  const ceiling = Math.max(...rows.flatMap((r) => r.horizon), Number.EPSILON);

  return (
    <ul className="m-0 list-none p-0">
      {rows.map((c) => (
        <li key={c.element}>
          <button
            type="button"
            onClick={() => onSelect?.(c)}
            aria-current={c.element === selected ? "true" : undefined}
            className={cn(
              "flex h-[52px] w-full items-center gap-2.5 border-b border-line-2 px-[11px] text-left",
              "focus-visible:outline focus-visible:-outline-offset-2 focus-visible:outline-fg-2",
              c.element === selected
                ? "bg-panel-3 shadow-[inset_2px_0_0_var(--bs-fg)]"
                : "hover:bg-hl",
            )}
          >
            <span className="w-3.5 shrink-0 text-right font-mono text-[10px] tabular text-fg-3">
              {c.rank}
            </span>
            <RiskBar p={c.probability} />

            <span className="min-w-0 flex-1">
              <span className="block truncate text-[11px] tracking-[-0.005em] text-fg">
                {c.name}
              </span>
              <span className="mt-[3px] flex items-center gap-[7px] font-mono text-[9.5px] tabular text-fg-3">
                <i
                  aria-hidden="true"
                  className="size-1 shrink-0 rounded-full bg-ok"
                  title="provenance chain complete"
                />
                <QuantityValue q={c.voltage} decimals={0} className="text-[9.5px]" />
                <QuantityValue q={c.length} className="text-[9.5px]" />
              </span>
            </span>

            <Sparkline points={c.horizon} max={ceiling} label={`${c.name} risk over the forecast horizon`} />

            <span className="shrink-0 text-right">
              <ProbabilityValue p={c.probability} className="text-xs" />
              <span className="mt-[3px] block font-mono text-[9.5px] tabular text-fg-2">
                <QuantityValue q={c.expectedUnservedEnergy} decimals={1} className="text-[9.5px]" />
              </span>
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}
