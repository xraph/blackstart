/**
 * Cascade playback.
 *
 * The playhead is local state rather than a search param. It moves many times
 * a second and would flood the history stack; the *run* is what belongs in
 * the URL, not the frame within it.
 *
 * The scrubber keys on event position rather than linear time, because
 * cascades are bursty — a linear axis spends most of its width on the seconds
 * where nothing happens, which is exactly the part nobody needs to scrub
 * through.
 */

import { useEffect, useMemo, useRef, useState } from "react";
import {
  BottomSheet,
  CascadeCounters,
  EventDensityTrack,
  EventLog,
  GlassPanel,
  IslandLegend,
  QuantityValue,
  RAIL_BREAKPOINT,
  SideRail,
  Transport,
  useMediaQuery,
} from "@blackstart/ui";
import { Scene } from "@blackstart/globe";

import { useViewerModel } from "../model/use-viewer-model";
import { CASCADE, FREQ_TRACES } from "../fixtures/derecho";

/** Simulated seconds advanced per real second at 1×. */
const BASE_RATE = 1.2;
const SPEEDS = [0.25, 1, 4] as const;

export function CascadeScreen(): React.JSX.Element {
  const { model } = useViewerModel();
  const wide = useMediaQuery(RAIL_BREAKPOINT);
  const [sheetOpen, setSheetOpen] = useState(false);

  const lastT = CASCADE.events[CASCADE.events.length - 1]?.t ?? 1;
  const [position, setPosition] = useState(1);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState<(typeof SPEEDS)[number]>(1);

  /* requestAnimationFrame rather than setInterval: a timer keeps firing in a
   * background tab, so the run silently races to the end while nobody is
   * watching and the reader comes back to a finished cascade. */
  const frame = useRef<number>(0);
  useEffect(() => {
    if (!playing) return undefined;
    let previous = performance.now();

    const tick = (now: number): void => {
      const elapsed = (now - previous) / 1000;
      previous = now;
      setPosition((p) => {
        const next = p + (elapsed * BASE_RATE * speed) / lastT;
        if (next >= 1) {
          setPlaying(false);
          return 1;
        }
        return next;
      });
      frame.current = requestAnimationFrame(tick);
    };

    frame.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame.current);
  }, [playing, speed, lastT]);

  const simTime = position * lastT;
  const shown = useMemo(() => CASCADE.events.filter((e) => e.t <= simTime + 1e-6), [simTime]);

  /** Steps to the adjacent discrete event, not a fixed time delta. */
  const step = (delta: -1 | 1): void => {
    setPlaying(false);
    const times = CASCADE.events.map((e) => e.t);
    const next =
      delta === 1
        ? times.find((t) => t > simTime + 1e-6)
        : [...times].reverse().find((t) => t < simTime - 1e-6);
    if (next !== undefined) setPosition(next / lastT);
  };

  const formatted = `t+${String(Math.floor(simTime / 60)).padStart(2, "0")}:${(simTime % 60)
    .toFixed(2)
    .padStart(5, "0")}`;

  const log = <EventLog events={shown} />;

  return (
    <div className="flex h-full flex-col">
      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1 overflow-hidden bg-stage">
          <Scene model={model} onSelect={() => undefined} />

          <GlassPanel
            title="Islands"
            meta={`${CASCADE.islands.length} formed`}
            className="right-3 top-3 w-[212px] max-md:hidden"
          >
            <IslandLegend islands={CASCADE.islands} />
          </GlassPanel>

          <GlassPanel title="Blast radius" className="left-3 top-3 w-[196px] max-md:hidden">
            <div className="p-2.5">
              <QuantityValue
                q={CASCADE.blastRadius}
                className="text-[22px] tracking-[-0.03em] text-fg"
              />
              <div className="mt-2 border-t border-line-2 pt-2 font-mono text-[9px] leading-relaxed text-fg-3">
                <span className="text-fg-2">counties</span> {CASCADE.countiesAffected} affected
                <br />
                <span className="text-fg-2">initiating</span> {CASCADE.initiating}
              </div>
            </div>
          </GlassPanel>

          {!wide && (
            <BottomSheet
              title="Event sequence"
              open={sheetOpen}
              onOpenChange={setSheetOpen}
              peek={
                <span className="font-mono text-[10px] tabular text-fg-2">
                  {shown.length} of {CASCADE.events.length} events · {formatted}
                </span>
              }
            >
              {log}
            </BottomSheet>
          )}
        </div>

        {wide && (
          <SideRail
            title="Event sequence"
            width={360}
            count={`${shown.length} / ${CASCADE.events.length}`}
            subtitle={
              <>
                t+00:00.00 → {formatted}
                <span className="ml-auto">REVERSE ▾</span>
              </>
            }
          >
            {log}
          </SideRail>
        )}
      </div>

      {/* The counter strip is wider than a phone. It scrolls rather than
          wrapping, because the three totals read as one row. */}
      <div className="overflow-x-auto">
        <CascadeCounters run={CASCADE} traces={FREQ_TRACES} />
      </div>

      <div className="flex h-16 shrink-0 items-stretch border-t border-line bg-panel">
        <div className="w-[150px] shrink-0 border-r border-line pl-3 pt-3 max-md:hidden">
          <div className="font-mono text-[9px] uppercase tracking-[0.13em] text-fg-3">
            Sim time
          </div>
          <div className="mt-1 font-mono text-[12.5px] tracking-[-0.02em] tabular text-fg">
            {formatted}
          </div>
        </div>

        <EventDensityTrack events={CASCADE.events} position={position} />

        <div className="flex w-[212px] shrink-0 flex-col gap-1.5 border-l border-line px-3 pt-3 max-md:w-auto">
          <Transport
            playing={playing}
            onToggle={() => setPlaying((p) => !p)}
            onStep={step}
            trailing={
              <div className="ml-1 flex overflow-hidden rounded-[2px] border border-line-2">
                {SPEEDS.map((s) => (
                  <button
                    key={s}
                    type="button"
                    onClick={() => setSpeed(s)}
                    aria-pressed={s === speed}
                    className={`h-[19px] border-r border-line-2 px-[7px] font-mono text-[9.5px] tabular last:border-r-0 ${
                      s === speed ? "bg-panel-3 text-fg" : "text-fg-3"
                    }`}
                  >
                    {s}×
                  </button>
                ))}
              </div>
            }
          />
          <div className="font-mono text-[9px] tabular text-fg-3 max-md:hidden">
            <span className="text-fg-2">SOLVER</span> bs-cascade 0.3.1
          </div>
        </div>
      </div>
    </div>
  );
}
