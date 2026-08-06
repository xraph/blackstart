/**
 * The cascade event log and its density track.
 *
 * The log stays achromatic apart from the keyword column and the island badge:
 * severity is read from the word, and colour is reserved for island identity,
 * which is the thing that genuinely needs a categorical encoding.
 *
 * The track underneath keys on event position rather than linear time.
 * Cascades are bursty — a linear scrubber spends most of its width on the
 * seconds where nothing happens, which is the part nobody needs to scrub.
 */

import type { ReactElement } from "react";
import {
  CASCADE_PHASES,
  newestFirst,
  type CascadeEvent,
  type CascadeEventKind,
  type IslandId,
} from "@blackstart/schema";
import { cn } from "../cn";
import { islandBg, islandVar } from "../lib/palette";

const KIND_TONE: Record<CascadeEventKind, string> = {
  trip: "text-risk-4",
  overload: "text-risk-3",
  island: "text-island-1",
  shed: "text-risk-2",
  stabilise: "text-ok",
};

const KIND_MARK: Record<CascadeEventKind, { readonly colour: string; readonly height: number }> = {
  trip: { colour: "var(--bs-risk-4)", height: 16 },
  overload: { colour: "var(--bs-risk-3)", height: 10 },
  island: { colour: "var(--bs-island-1)", height: 14 },
  shed: { colour: "var(--bs-risk-2)", height: 10 },
  stabilise: { colour: "var(--bs-ok)", height: 10 },
};

function formatSimTime(t: number): string {
  const m = Math.floor(t / 60);
  const s = (t % 60).toFixed(2).padStart(5, "0");
  return `t+${String(m).padStart(2, "0")}:${s}`;
}

export type EventOrder = "newest" | "oldest";

export function EventLog({
  events,
  order = "newest",
  highlightCount = 2,
  onHover,
}: {
  readonly events: readonly CascadeEvent[];
  /**
   * Which end the log reads from.
   *
   * The rail's control said `REVERSE`, which never named the order it would
   * produce — or the one it would be reversing from. Both values here state
   * an order, and the default is the one the log has always used.
   */
  readonly order?: EventOrder;
  /** How many of the most recent rows are tinted as just-arrived. */
  readonly highlightCount?: number;
  readonly onHover?: (e: CascadeEvent | null) => void;
}): ReactElement {
  const rows = order === "newest" ? newestFirst(events) : [...events];

  return (
    <ol className="m-0 list-none p-0 font-mono text-[9.5px] tabular">
      {rows.map((e, i) => (
        <li
          key={`${e.t}-${e.element}-${e.kind}`}
          onMouseEnter={() => onHover?.(e)}
          onMouseLeave={() => onHover?.(null)}
          className={cn(
            "flex h-[26px] items-center border-b border-line-t px-[11px] text-fg-2",
            /* Counted from the newest event, not from the top of the screen.
               The tint means "just arrived", and keyed on position it would
               mark the *oldest* events in oldest-first order — the opposite
               claim, made silently. */
            (order === "newest" ? i : rows.length - 1 - i) < highlightCount && "bg-hl",
          )}
        >
          <span className="w-[52px] shrink-0 text-fg-3">{formatSimTime(e.t)}</span>
          <span className={cn("w-[46px] shrink-0 tracking-[0.06em]", KIND_TONE[e.kind])}>
            {e.kind.toUpperCase()}
          </span>
          <span className="flex-1 truncate text-fg">{e.element}</span>
          {e.island !== undefined && <IslandBadge id={e.island} />}
          <span className="shrink-0 pl-1.5 text-fg-3">{e.detail}</span>
        </li>
      ))}
    </ol>
  );
}

export function IslandBadge({ id }: { readonly id: IslandId }): ReactElement {
  return (
    <span
      className={cn(
        "ml-1.5 inline-flex h-[13px] shrink-0 items-center rounded-[2px] px-1",
        "text-[8.5px] font-bold text-white",
        islandBg(id),
      )}
    >
      I{id}
    </span>
  );
}

/**
 * The event-density track.
 *
 * Phase labels sit under it so the shape of a cascade — a trip, a burst of
 * redistribution, separation, shedding, then settling — is legible at rest
 * without pressing play.
 */
export function EventDensityTrack({
  events,
  position,
}: {
  readonly events: readonly CascadeEvent[];
  /** Playhead, 0 to 1. */
  readonly position: number;
}): ReactElement {
  const last = events[events.length - 1];
  const span = last === undefined || last.t === 0 ? 1 : last.t;

  return (
    <div className="relative min-w-0 flex-1 px-4">
      <div className="absolute inset-x-4 top-[38px] h-px bg-line-2" />
      <div
        className="absolute left-4 top-[38px] h-px bg-fg-3"
        style={{ width: `calc(${position * 100}% - 2rem)` }}
      />
      {events.map((e) => {
        const mark = KIND_MARK[e.kind];
        return (
          <span
            key={`${e.t}-${e.element}`}
            aria-hidden="true"
            className="absolute bottom-5 w-0.5 rounded-[1px]"
            style={{
              left: `calc(1rem + ${(e.t / span) * 100}% - ${(e.t / span) * 2}rem)`,
              height: mark.height,
              background: e.island === undefined ? mark.colour : islandVar(e.island),
            }}
          />
        );
      })}
      {CASCADE_PHASES.map((phase, i) => (
        <span
          key={phase}
          className="absolute top-2 -translate-x-1/2 whitespace-nowrap font-mono text-[8.5px] uppercase tracking-[0.09em] text-fg-4"
          style={{ left: `${[2, 26, 66, 88, 99][i] ?? 0}%` }}
        >
          {phase}
        </span>
      ))}
      <span
        className="absolute top-3.5 h-[34px] w-px bg-fg after:absolute after:-left-1 after:-top-1 after:h-[5px] after:w-[9px] after:rounded-[1.5px] after:bg-fg after:content-['']"
        style={{ left: `calc(1rem + ${position * 100}% - ${position * 2}rem)` }}
      />
    </div>
  );
}
