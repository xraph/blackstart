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
 *
 * The run itself comes from the platform, not a fixture. `useCascadeRunsGet`
 * returns the wire shape the solver actually emits — bare numbers, no island
 * id, a proto-spelled event kind — and `toCascadeRun` below is the one place
 * that gets translated into `CascadeRun`, so every component downstream can
 * keep assuming units and citations rather than re-deriving them.
 */

import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
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
import { useCascadeRunsGet, type Run as WireRun } from "@blackstart/api-client";
import { CascadeRun, provenanceOf, hz, mw, type CascadeEventKind } from "@blackstart/schema";

import { cascadeRoute } from "../router";
import { runsSearch } from "../search/params";
import { useApiClient } from "../api/provider";
import { useViewerModel } from "../model/use-viewer-model";

/** Simulated seconds advanced per real second at 1×. */
const BASE_RATE = 1.2;
const SPEEDS = [0.25, 1, 4] as const;

/* Parsed rather than written out, so a link to the runs index carries
 * whatever defaults that screen's own schema declares instead of a second,
 * driftable copy of them — the same reasoning the root route uses for its
 * landing redirect. */
const DEFAULT_RUNS_SEARCH = runsSearch.parse({});

/**
 * The proto enum spells this event "stabilize"; the schema and every
 * user-facing string spell it "stabilise". One table, here, rather than two
 * spellings of the same event drifting apart across the wire boundary.
 */
const WIRE_EVENT_KIND: Record<string, CascadeEventKind> = {
  overload: "overload",
  trip: "trip",
  island: "island",
  shed: "shed",
  stabilize: "stabilise",
  stabilise: "stabilise",
};

/**
 * Translates one fetched run into the domain shape the playback surface
 * expects.
 *
 * `undefined` on failure rather than throwing: a run the platform returned
 * but that does not satisfy the cascade contract is a data problem, not a
 * crash, and the screen renders it the same way it renders a fetch failure —
 * see the RULE in the cascade brief about a failure never looking like
 * nothing happened.
 */
function toCascadeRun(wire: WireRun): CascadeRun | undefined {
  const result = CascadeRun.safeParse({
    provenance: provenanceOf({
      modelHash: wire.modelHash,
      solverVersion: wire.solverVersion,
      run: wire.id,
    }),
    run: wire.id,
    // The wire lists every branch taken out at t=0; the domain names the run
    // by one descriptive string, which is what an operator actually reads.
    initiating: wire.initiating.join(", ") || "unknown",
    events: wire.events.map((e) => ({
      t: e.at,
      kind: WIRE_EVENT_KIND[e.kind] ?? e.kind,
      element: e.element,
      detail: e.detail,
      island: e.island,
    })),
    // The wire carries no explicit island id: the solver forms islands in
    // the order it reports them, and events reference that same order, so
    // position in the array *is* the id.
    islands: wire.islands.map((isl, i) => ({
      id: i + 1,
      balance: mw(isl.balance),
      frequency: isl.frequency === undefined ? undefined : hz(isl.frequency),
      nadir: isl.nadir === undefined ? undefined : hz(isl.nadir),
      state: isl.condition,
      busCount: isl.busCount,
      trace: isl.trace,
    })),
    loadLost: mw(wire.loadLost),
  });

  return result.success ? result.data : undefined;
}

export function CascadeScreen(): React.JSX.Element {
  const search = cascadeRoute.useSearch();
  const client = useApiClient();
  const { model } = useViewerModel();
  const wide = useMediaQuery(RAIL_BREAKPOINT);
  const [sheetOpen, setSheetOpen] = useState(false);

  /* Disabled rather than skipped: a query hook has to run on every render, so
   * an absent run param is expressed to react-query as `enabled: false`
   * rather than by not calling the hook at all. */
  const { data, isError, error } = useCascadeRunsGet(client, search.run ?? "", {
    enabled: search.run !== undefined,
  });

  const run = useMemo(() => (data === undefined ? undefined : toCascadeRun(data)), [data]);
  /* The platform answered but the payload does not parse as a cascade run —
   * as real a failure as the fetch never landing, and shown the same way. */
  const malformed = data !== undefined && run === undefined;

  const events = run?.events ?? [];
  const lastT = events[events.length - 1]?.t ?? 1;
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
  const shown = useMemo(() => events.filter((e) => e.t <= simTime + 1e-6), [events, simTime]);

  /** Steps to the adjacent discrete event, not a fixed time delta. */
  const step = (delta: -1 | 1): void => {
    setPlaying(false);
    const times = events.map((e) => e.t);
    const next =
      delta === 1
        ? times.find((t) => t > simTime + 1e-6)
        : [...times].reverse().find((t) => t < simTime - 1e-6);
    if (next !== undefined) setPosition(next / lastT);
  };

  /* Absent, not empty-as-zero: an island with no trace has no frequency curve
   * to plot, and filtering it out here is what stops a flat line at 0 Hz from
   * standing in for "no data". */
  const traces = useMemo(
    () =>
      new Map(
        (run?.islands ?? [])
          .filter((i) => i.trace !== undefined && i.trace.length > 0)
          .map((i) => [i.id, i.trace!.map((p) => p.hz)]),
      ),
    [run],
  );

  const formatted = `t+${String(Math.floor(simTime / 60)).padStart(2, "0")}:${(simTime % 60)
    .toFixed(2)
    .padStart(5, "0")}`;

  const log = <EventLog events={shown} />;

  // No run named in the URL: this is a legitimate state, not a missing
  // fixture, so it points at where a run can be picked rather than guessing.
  if (search.run === undefined) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 bg-stage p-6 text-center">
        <p className="m-0 font-mono text-[10px] uppercase tracking-[0.13em] text-fg-3">
          No cascade run selected
        </p>
        <p className="m-0 max-w-[42ch] text-[12px] leading-relaxed text-fg-2">
          Pick a completed cascade from the{" "}
          <Link to="/runs" search={DEFAULT_RUNS_SEARCH} className="text-fg underline">
            runs index
          </Link>{" "}
          to play it back here.
        </p>
      </div>
    );
  }

  const failed = isError || malformed;
  // The commit link is the solver version the citation chain actually cites,
  // not a hand-typed label — reading it back here is what makes the footer
  // an assertion about *this* run instead of decoration.
  const solverRef = run?.provenance.find((l) => l.kind === "commit")?.ref ?? "—";

  return (
    <div className="flex h-full flex-col">
      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1 overflow-hidden bg-stage">
          {/* The globe draws the static network model, which does not depend
           * on the run — so it never shows a loading state of its own. */}
          <Scene model={model} onSelect={() => undefined} />

          {failed ? (
            <div
              role="alert"
              className="absolute inset-3 max-w-[420px] rounded-[4px] border border-alarm bg-panel/95 p-3.5 backdrop-blur-[6px]"
            >
              <div className="font-mono text-[10px] uppercase tracking-[0.13em] text-alarm">
                Cascade run failed to load
              </div>
              <p className="mt-2 text-[11.5px] leading-relaxed text-fg-2">
                {isError
                  ? error?.message
                  : "The platform returned a run that does not satisfy the cascade contract."}
              </p>
            </div>
          ) : (
            <>
              <GlassPanel
                title="Islands"
                meta={run === undefined ? "loading…" : `${run.islands.length} formed`}
                className="right-3 top-3 w-[212px] max-md:hidden"
              >
                {run === undefined ? (
                  <p className="p-2.5 text-[11px] text-fg-3">Loading run…</p>
                ) : (
                  <IslandLegend islands={run.islands} />
                )}
              </GlassPanel>

              <GlassPanel title="Blast radius" className="left-3 top-3 w-[196px] max-md:hidden">
                <div className="p-2.5">
                  {run === undefined ? (
                    <p className="m-0 text-[11px] text-fg-3">Loading run…</p>
                  ) : (
                    <>
                      <QuantityValue
                        q={run.blastRadius}
                        className="text-[22px] tracking-[-0.03em] text-fg"
                      />
                      <div className="mt-2 border-t border-line-2 pt-2 font-mono text-[9px] leading-relaxed text-fg-3">
                        <span className="text-fg-2">counties</span>{" "}
                        {run.countiesAffected ?? "not computed"} affected
                        <br />
                        <span className="text-fg-2">initiating</span> {run.initiating}
                      </div>
                    </>
                  )}
                </div>
              </GlassPanel>

              {!wide && (
                <BottomSheet
                  title="Event sequence"
                  open={sheetOpen}
                  onOpenChange={setSheetOpen}
                  peek={
                    <span className="font-mono text-[10px] tabular text-fg-2">
                      {shown.length} of {events.length} events · {formatted}
                    </span>
                  }
                >
                  {log}
                </BottomSheet>
              )}
            </>
          )}
        </div>

        {wide && !failed && (
          <SideRail
            title="Event sequence"
            width={360}
            count={`${shown.length} / ${events.length}`}
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
        {run === undefined ? (
          <div className="flex h-[92px] shrink-0 items-center border-t border-line bg-panel px-5 font-mono text-[11px] text-fg-3">
            {failed ? "Run unavailable." : "Loading run…"}
          </div>
        ) : (
          <CascadeCounters run={run} traces={traces} />
        )}
      </div>

      <div className="flex h-16 shrink-0 items-stretch border-t border-line bg-panel">
        <div className="w-[150px] shrink-0 border-r border-line pl-3 pt-3 max-md:hidden">
          <div className="font-mono text-[9px] uppercase tracking-[0.13em] text-fg-3">Sim time</div>
          <div className="mt-1 font-mono text-[12.5px] tracking-[-0.02em] tabular text-fg">
            {formatted}
          </div>
        </div>

        <EventDensityTrack events={events} position={position} />

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
            <span className="text-fg-2">SOLVER</span> {solverRef}
          </div>
        </div>
      </div>
    </div>
  );
}
