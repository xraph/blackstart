/**
 * The scenario runs grid.
 *
 * Valid time and knowledge time are separate columns and are never merged.
 * Merging them is the silent-leakage bug this project exists to avoid, and a
 * grid that shows one timestamp cannot distinguish a live forecast from a
 * backtest — so the two columns are structural, not configurable.
 */

import type { ReactElement } from "react";
import { formatInstant, formatWall, isBacktest, type ScenarioRun } from "@blackstart/schema";
import { cn } from "../cn";
import { QuantityValue } from "../instrument/quantity";
import { ReproBadge, StatusGlyph } from "../instrument/status";

const TH =
  "sticky top-0 h-[30px] whitespace-nowrap border-b border-line-2 bg-panel px-2.5 text-left text-[9px] font-normal uppercase tracking-[0.1em] text-fg-3";
const TD = "touch-row h-8 whitespace-nowrap border-b border-line-t px-2.5 text-fg-2";

export function RunsGrid({
  runs,
  selected,
  onToggle,
}: {
  readonly runs: readonly ScenarioRun[];
  readonly selected: ReadonlySet<string>;
  readonly onToggle?: (id: string) => void;
}): ReactElement {
  return (
    <table className="w-full border-collapse font-mono text-[10.5px] tabular">
      <thead>
        <tr>
          <th className={cn(TH, "w-[26px]")}>
            <span className="sr-only">Select</span>
          </th>
          <th className={TH}>Run</th>
          <th className={TH}>Scenario</th>
          <th className={TH}>Valid time</th>
          <th className={TH}>Knowledge time</th>
          <th className={TH}>Solver</th>
          <th className={TH}>Model</th>
          <th className={TH}>Status</th>
          <th className={cn(TH, "text-right")}>Cont.</th>
          <th className={cn(TH, "text-right")}>EUE</th>
          <th className={cn(TH, "text-right")}>Wall</th>
          <th className={cn(TH, "w-14")}>Repro</th>
        </tr>
      </thead>
      <tbody>
        {runs.map((r) => {
          const on = selected.has(r.id);
          /* A backtest is identifiable by scanning one column, not by reading
           * the scenario name and hoping someone spelled it consistently. */
          const back =
            r.temporal.validTime !== undefined &&
            isBacktest({ validTime: r.temporal.validTime, knowledgeTime: r.temporal.knowledgeTime });
          const timeTone = back ? "text-amber" : undefined;

          return (
            <tr key={r.id} className={cn(on && "bg-panel-3")}>
              <td className={cn(TD, on && "shadow-[inset_2px_0_0_var(--bs-fg)]")}>
                <input
                  type="checkbox"
                  checked={on}
                  onChange={() => onToggle?.(r.id)}
                  aria-label={`Select run ${r.id}`}
                  className="size-[11px] accent-[var(--bs-fg)]"
                />
              </td>
              <td className={cn(TD, "text-fg")}>{r.id}</td>
              <td className={TD}>{r.scenario}</td>
              <td className={cn(TD, timeTone)}>
                {r.temporal.validTime === undefined ? "—" : formatInstant(r.temporal.validTime)}
              </td>
              <td className={cn(TD, timeTone)}>{formatInstant(r.temporal.knowledgeTime)}</td>
              <td className={TD}>{r.solverVersion}</td>
              <td className={TD}>{r.modelVersion ?? "—"}</td>
              <td className={TD}>
                <StatusGlyph status={r.status} />
              </td>
              <td className={cn(TD, "text-right")}>{r.contingenciesSolved || "—"}</td>
              <td className={cn(TD, "text-right")}>
                {r.expectedUnservedEnergy === null ? (
                  "—"
                ) : (
                  <QuantityValue q={r.expectedUnservedEnergy} unitless className="text-[10.5px]" />
                )}
              </td>
              <td className={cn(TD, "text-right")}>{formatWall(r.wallMs)}</td>
              <td className={TD}>
                <ReproBadge verdict={r.repro} />
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
