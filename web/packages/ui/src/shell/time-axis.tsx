/**
 * The dual-track time axis.
 *
 * Two tracks, not one. The upper is valid time — the forecast hour every layer
 * including sun position follows. The lower is knowledge time: an as-of marker
 * pinned to the present by default, which dragging left puts the whole console
 * into backtest, showing only what was known at that moment.
 *
 * Collapsing these into a single scrubber is the obvious simplification and it
 * is exactly the one that makes a backtest indistinguishable from a live view.
 * The component takes both or it does not render.
 */

import type { ReactElement } from "react";
import {
  formatInstant,
  temporalMode,
  type BitemporalRef,
  type ForecastHorizon,
} from "@blackstart/schema";
import { cn } from "../cn";

export interface TimeAxisProps {
  readonly temporal: BitemporalRef;
  readonly horizon: ForecastHorizon;
  /** Lead hour currently displayed; must appear in the horizon. */
  readonly leadHour: number;
  readonly onScrubValid?: (leadHour: number) => void;
  readonly onScrubKnowledge?: (t: string) => void;
  readonly transport?: ReactElement;
}

export function TimeAxis({
  temporal,
  horizon,
  leadHour,
  onScrubValid,
  transport,
}: TimeAxisProps): ReactElement {
  const hours = horizon.leadHours;
  const first = hours[0] ?? 0;
  const last = hours[hours.length - 1] ?? 1;
  const span = last - first || 1;
  const pct = ((leadHour - first) / span) * 100;
  const mode = temporalMode(temporal);

  return (
    <div className="flex h-14 shrink-0 items-stretch border-t border-line bg-panel">
      <div className="w-[150px] shrink-0 border-r border-line pl-3 pt-2">
        <div className="font-mono text-[9px] uppercase tracking-[0.13em] text-fg-3">
          Valid time
        </div>
        <div className="mt-1 font-mono text-[12.5px] tracking-[-0.02em] tabular text-fg">
          {formatInstant(temporal.validTime)}
        </div>
      </div>

      <div className="flex min-w-0 flex-1 flex-col px-4 pt-2.5">
        {/* valid time */}
        <div className="relative h-5">
          <div className="absolute inset-x-0 -top-0.5 h-2.5">
            {hours
              .filter((h) => (h - first) % 3 === 0)
              .map((h) => (
                <span
                  key={h}
                  className="absolute -translate-x-1/2 font-mono text-[8.5px] tabular text-fg-3"
                  style={{ left: `${((h - first) / span) * 100}%` }}
                >
                  f+{String(h).padStart(2, "0")}
                </span>
              ))}
          </div>
          <div className="absolute inset-x-0 top-[13px] h-px bg-line-2" />
          <div className="absolute left-0 top-[13px] h-px bg-fg-3" style={{ width: `${pct}%` }} />
          {hours.map((h) => (
            <button
              key={h}
              type="button"
              aria-label={`forecast hour ${h}`}
              onClick={() => onScrubValid?.(h)}
              className="absolute top-2 h-1.5 w-px bg-line-3 focus-visible:outline focus-visible:outline-1 focus-visible:outline-fg-2"
              style={{ left: `${((h - first) / span) * 100}%` }}
            />
          ))}
          <div
            className="absolute top-1 h-[19px] w-0.5 rounded-[1px] bg-fg after:absolute after:-left-1 after:-top-1 after:h-[5px] after:w-2.5 after:rounded-[1.5px] after:bg-fg after:content-['']"
            style={{ left: `${pct}%` }}
          />
        </div>

        {/* knowledge time */}
        <div className="relative mt-0.5 h-4">
          <span className="absolute -top-px left-0 font-mono text-[8.5px] tracking-[0.08em] text-fg-4">
            KNOWLEDGE TIME
          </span>
          <div className="absolute inset-x-0 top-2 h-px bg-line" />
          <div
            className="absolute left-0 top-2 h-px w-full"
            style={{ background: "var(--bs-amber-line)" }}
          />
          <div className="absolute right-0 top-0.5 h-3.5 w-0.5 rounded-[1px] bg-amber after:absolute after:-left-[3.5px] after:-top-[3px] after:h-1 after:w-[9px] after:rounded-[1.5px] after:bg-amber after:content-['']" />
          <span
            className={cn(
              "absolute -top-px right-0 font-mono text-[8.5px] tracking-[0.08em]",
              mode === "live" ? "text-amber" : "text-amber",
            )}
          >
            {mode === "live" ? "NOW · LIVE" : `AS OF ${formatInstant(temporal.knowledgeTime)}`}
          </span>
        </div>
      </div>

      <div className="flex w-[196px] shrink-0 flex-col gap-1.5 border-l border-line px-3 pt-2">
        {transport}
        <div className="font-mono text-[9px] tabular leading-relaxed text-fg-3">
          <span className="text-fg-2">CYCLE</span> {horizon.cycle} ·{" "}
          <span className="text-fg-2">ISSUED</span> {formatInstant(horizon.issued)}
        </div>
      </div>
    </div>
  );
}

/** Play / pause / step, shared by the time axis and the cascade transport. */
export function Transport({
  playing,
  onToggle,
  onStep,
  trailing,
}: {
  readonly playing: boolean;
  readonly onToggle?: () => void;
  readonly onStep?: (delta: -1 | 1) => void;
  readonly trailing?: ReactElement;
}): ReactElement {
  const btn =
    "flex h-5 w-[22px] items-center justify-center rounded-[2px] border border-line-2 bg-panel-2 text-fg-2 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2";
  return (
    <div className="flex gap-1">
      <button type="button" className={btn} aria-label="Step back" onClick={() => onStep?.(-1)}>
        <svg viewBox="0 0 10 10" className="size-[9px] fill-current">
          <path d="M8 1v8L2 5z" />
          <rect x="1" y="1" width="1.4" height="8" />
        </svg>
      </button>
      <button
        type="button"
        className={cn(btn, "border-fg bg-fg text-ink")}
        aria-label={playing ? "Pause" : "Play"}
        onClick={onToggle}
      >
        <svg viewBox="0 0 10 10" className="size-[9px] fill-current">
          {playing ? (
            <>
              <rect x="2" y="1" width="2.2" height="8" />
              <rect x="5.8" y="1" width="2.2" height="8" />
            </>
          ) : (
            <path d="M2 1l7 4-7 4z" />
          )}
        </svg>
      </button>
      <button type="button" className={btn} aria-label="Step forward" onClick={() => onStep?.(1)}>
        <svg viewBox="0 0 10 10" className="size-[9px] fill-current">
          <path d="M2 1v8l6-4z" />
          <rect x="7.6" y="1" width="1.4" height="8" />
        </svg>
      </button>
      {trailing}
    </div>
  );
}
