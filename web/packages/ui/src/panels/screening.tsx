/**
 * The N-1 screening rail.
 *
 * Not `ContingencyRail`, which ranks by expected cost. That rail is the phase-6
 * surface and the platform publishes neither a probability nor an expected
 * cost — `grep -i probability` over the whole generated client returns
 * nothing. Rendering this data under that header would mean inventing the
 * numbers in between, so the rail names its own quantity instead.
 *
 * Rows are ordered by whoever called `sortScreening`; this renders what it is
 * given, the way `ContingencyRail` does.
 */

import type { ReactElement } from "react";
import { evaluated, type ScreeningCandidate, type ScreeningList } from "@blackstart/schema";
import { cn } from "../cn";

/** What the screen could not say, in the words an operator would use. */
const UNEVALUATED: Record<string, string> = {
  radial: "radial — islands the network",
  "not-applicable": "already out of service",
};

/**
 * Loading as a proportion of rating, quantised to the console's severity ramp.
 *
 * The action threshold is 1.0 — at rating — and the bands above it are where
 * a reader is deciding. Below rating everything is one band, because the
 * difference between 40% and 60% loaded is not a difference anybody acts on.
 */
function tone(loading: number): string {
  if (loading >= 1.5) return "text-risk-4";
  if (loading >= 1.2) return "text-risk-3";
  if (loading >= 1.0) return "text-risk-2";
  return "text-fg-2";
}

export function ScreeningRail({
  rows,
  selected,
  onSelect,
}: {
  readonly rows: ScreeningList;
  readonly selected?: string;
  readonly onSelect?: (c: ScreeningCandidate) => void;
}): ReactElement {
  if (rows.length === 0) {
    return (
      <p className="p-3 text-[11px] leading-snug text-fg-2">
        This run screened nothing that warrants verification. Switch the filter to{" "}
        <span className="font-mono text-fg">ALL</span> to see the outages it cleared.
      </p>
    );
  }

  return (
    <ul className="m-0 list-none p-0">
      {rows.map((c) => {
        const unknown = !evaluated(c.screenability);

        return (
          <li key={c.contingencyId}>
            <button
              type="button"
              onClick={() => onSelect?.(c)}
              aria-current={c.contingencyId === selected ? "true" : undefined}
              className={cn(
                "flex h-[52px] w-full items-center gap-2.5 border-b border-line-2 px-[11px] text-left",
                "focus-visible:outline focus-visible:-outline-offset-2 focus-visible:outline-fg-2",
                c.contingencyId === selected
                  ? "bg-panel-3 shadow-[inset_2px_0_0_var(--bs-fg)]"
                  : "hover:bg-hl",
              )}
            >
              {/* Amber for the unevaluated, which is the frame's own colour
                  for "this is not what it looks like". They are not safe;
                  they are unknown, and they sort first for that reason. */}
              <span
                aria-hidden="true"
                className={cn(
                  "h-7 w-0.5 shrink-0 rounded-[1px]",
                  unknown ? "bg-amber" : "bg-line-3",
                )}
              />

              <span className="min-w-0 flex-1">
                {/* A branch index, because MATPOWER branches have no
                    identifier of their own. Terse and true. */}
                <span className="block truncate font-mono text-[11px] text-fg">
                  branch {c.branch}
                </span>
                <span className="mt-[3px] block truncate font-mono text-[9.5px] text-fg-3">
                  {unknown
                    ? (UNEVALUATED[c.screenability] ?? c.screenability)
                    : c.violations.length === 0
                      ? "no violations"
                      : `${c.violations.length} over rating`}
                </span>
              </span>

              <span className="shrink-0 text-right">
                {c.worstLoading === undefined ? (
                  /* An em dash, not a zero. The screen could not look, and a
                     zero here would rank as the safest thing on the list. */
                  <span
                    className="block font-mono text-xs tabular text-amber"
                    title="not evaluated"
                  >
                    —
                  </span>
                ) : (
                  <span className={cn("block font-mono text-xs tabular", tone(c.worstLoading))}>
                    {(c.worstLoading * 100).toFixed(0)}%
                  </span>
                )}
                <span className="mt-[3px] block font-mono text-[9px] uppercase tracking-[0.08em] text-fg-3">
                  of rating
                </span>
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}
