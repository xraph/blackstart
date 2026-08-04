/**
 * The operator console.
 *
 * Full-bleed globe with chrome anchored to the corners rather than scattered
 * across it. Selecting a span sets a search param rather than component
 * state, so the inspector is reachable by URL and a review conversation can
 * link straight to the element being discussed.
 *
 * The inspector replaces the rail rather than opening over the globe. Spatial
 * context — where this span sits relative to the storm and to everything else
 * at risk — is most of what the reader is using to decide, and a modal
 * covering it defeats the panel it opens.
 */

import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { AnyTypeId } from "@blackstart/schema";
import { Scene, type Selection } from "@blackstart/globe";
import {
  BottomSheet,
  ContingencyRail,
  ElementInspector,
  GlassPanel,
  LayerStackPanel,
  RiskRamp,
  RAIL_BREAKPOINT,
  SideRail,
  TimeAxis,
  Transport,
  layerSummary,
  useMediaQuery,
} from "@blackstart/ui";

import { consoleRoute } from "../router";
import { temporalOf } from "../search/params";
import { useViewerModel } from "../model/use-viewer-model";
import { useRouteGeometry } from "../api/use-route-geometry";
import {
  CONTINGENCIES,
  HORIZON,
  INSPECTED,
  LAYERS,
  LINE_TULIA_LEVELLAND,
} from "../fixtures/derecho";

export function ConsoleScreen(): React.JSX.Element {
  const search = consoleRoute.useSearch();
  const navigate = useNavigate({ from: consoleRoute.fullPath });
  const { model, problem } = useViewerModel();
  const geometry = useRouteGeometry();
  const [, setPicked] = useState<Selection | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);

  const temporal = temporalOf(search);
  /* An explicit knowledge time is the signal, not a clock comparison: the
   * reader asked to be pinned, so say so regardless of how stale it is. */
  const asOf = search.known !== undefined;

  const select = (element: string): void => {
    void navigate({ search: (prev) => ({ ...prev, sel: element }) });
  };
  const clear = (): void => {
    void navigate({ search: ({ sel: _dropped, ...rest }) => rest });
  };

  /* The globe reports ids from the exported model, which are not all TypeIDs.
   * One that is not addressable is a highlight, not a selection — writing it
   * to the URL would produce a link that resolves to nothing. */
  const pick = (selection: Selection | null): void => {
    setPicked(selection);
    if (selection === null) return;
    if (AnyTypeId.safeParse(selection.id).success) select(selection.id);
  };

  const explanation = search.sel === LINE_TULIA_LEVELLAND ? INSPECTED : null;
  const wide = useMediaQuery(RAIL_BREAKPOINT);

  /* The rail's *contents*, without chrome. The container supplies the header,
   * because a docked rail and a sheet each already have one and nesting them
   * would print the title twice. */
  const railBody =
    search.sel === undefined ? (
      <ContingencyRail rows={CONTINGENCIES} onSelect={(c) => select(c.element)} />
    ) : (
      <p className="p-3 text-[11px] leading-snug text-fg-2">
        No explanation is available for{" "}
        <span className="font-mono text-fg">{search.sel}</span>. The element is in the network
        but has not been scored in this run.
      </p>
    );

  const railTitle = search.sel === undefined ? "Ranked contingencies" : "Selection";

  return (
    <div
      className={`flex h-full flex-col ${asOf ? "outline outline-1 -outline-offset-1 outline-amber" : ""}`}
    >
      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1 overflow-hidden bg-stage">
          <Scene model={model} routes={geometry.routes} onSelect={pick} />

          <GlassPanel
            title="Layers"
            meta={layerSummary(LAYERS)}
            className="left-3 top-3 w-[264px] max-md:hidden"
          >
            <LayerStackPanel layers={LAYERS} />
          </GlassPanel>

          <GlassPanel
            title="Encoding — P(fail | forecast)"
            className="bottom-10 left-3 w-[228px] max-md:hidden"
          >
            <div className="p-2.5">
              <RiskRamp />
              <div className="mt-2.5 border-t border-line-2 pt-2 font-mono text-[9px] leading-relaxed text-fg-3">
                <span className="text-fg-2">width</span> voltage class 115 / 230 / 345 kV
                <br />
                <span className="text-fg-2">model</span> haz-gbst v0.4.2 · calib 0.031 ECE
                <br />
                <span className="text-fg-2">routes</span>{" "}
                {geometry.isPending
                  ? "loading…"
                  : geometry.error !== null
                    ? "unavailable"
                    : `${geometry.fetched} drawn${
                        geometry.fetched < geometry.available
                          ? ` of ${geometry.available}`
                          : ""
                      }`}
              </div>
            </div>
          </GlassPanel>

          {problem !== null && (
            <GlassPanel title="Network" className="left-3 top-3 w-[320px] md:left-[280px]">
              <p className="p-2.5 text-[11px] leading-snug text-amber">{problem}</p>
            </GlassPanel>
          )}

          {/* Narrow, with a selection: the inspector takes the whole stage.
              A 480px panel does not fit beside a globe on a phone, and half
              of each is worse than all of one. */}
          {!wide && explanation !== null && (
            <div className="absolute inset-0 z-30">
              <ElementInspector explanation={explanation} onClose={clear} />
            </div>
          )}

          {/* Narrow, without one: the rail reflows into a sheet. */}
          {!wide && explanation === null && (
            <BottomSheet
              title={railTitle}
              open={sheetOpen}
              onOpenChange={setSheetOpen}
              peek={
                <span className="font-mono text-[10px] tabular text-fg-2">
                  {CONTINGENCIES[0]?.name} · rank 1 of {CONTINGENCIES.length}
                </span>
              }
            >
              {railBody}
            </BottomSheet>
          )}
        </div>

        {wide &&
          (explanation !== null ? (
            <ElementInspector explanation={explanation} onClose={clear} />
          ) : (
            <SideRail
              title={railTitle}
              count={`${CONTINGENCIES.length} solved`}
              subtitle={
                <>
                  SORT
                  <span className="rounded-[2px] border border-line-2 px-1.5 text-fg-2">
                    EXPECTED COST ▾
                  </span>
                  <span className="ml-auto">
                    N-1 · f+{String(search.lead).padStart(2, "0")}
                  </span>
                </>
              }
            >
              {railBody}
            </SideRail>
          ))}
      </div>

      <TimeAxis
        temporal={temporal}
        horizon={HORIZON}
        leadHour={search.lead}
        mode={asOf ? "as-of" : "live"}
        onScrubValid={(lead) => void navigate({ search: (prev) => ({ ...prev, lead }) })}
        transport={<Transport playing={false} />}
      />
    </div>
  );
}
