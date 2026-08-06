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

import { useCallback, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { AnyTypeId } from "@blackstart/schema";
import { Scene, type Selection } from "@blackstart/globe";
import {
  BottomSheet,
  ContingencyRail,
  ElementInspector,
  GlassPanel,
  LayerStackPanel,
  MeasureRamp,
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
import { useHazardLayer } from "../api/use-hazard-layer";
import { useSelectedCase } from "../api/use-context-options";
import { AMPACITY, LOADING, colorOf } from "../api/hazard";
import { hazardLayerStack } from "./layers";
import { CONTINGENCIES, HORIZON, INSPECTED, LINE_TULIA_LEVELLAND } from "../fixtures/derecho";

export function ConsoleScreen(): React.JSX.Element {
  const search = consoleRoute.useSearch();
  const navigate = useNavigate({ from: consoleRoute.fullPath });
  const { model, problem } = useViewerModel();
  const geometry = useRouteGeometry();
  const [, setPicked] = useState<Selection | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);

  const encoding = search.layer === "loading" ? LOADING : AMPACITY;
  const hazard = useHazardLayer(geometry.routes, encoding, search.valid, search.known);

  /* Colour carries the quantity, alpha carries whether the conductor under it
   * was surveyed or assumed. A span with no value draws neutral rather than at
   * the safe end of the ramp — "nobody computed this" is not "this is fine". */
  const spanColor = useCallback(
    (spanId: string) => {
      const found = hazard.lookup(spanId);
      return colorOf(found?.value ?? null, found?.basis ?? null, encoding);
    },
    [hazard, encoding],
  );

  /* The layer stack's source column, from the model actually selected. It
   * read `ACTIVSg2000 v3` — a model this platform has never held — in the one
   * panel whose doc comment claims a screenshot of it is self-describing.
   *
   * Name and hash together, because a model is immutable and correcting one
   * produces a new model under the same name: the hash is what tells a
   * screenshot of this build from a screenshot of the next. */
  const networkCase = useSelectedCase(search.case);

  const layers = hazardLayerStack({
    encoding,
    issuance: hazard.issuance,
    rated: hazard.rated,
    unrated: hazard.unrated,
    network:
      networkCase === null
        ? "no case selected"
        : `${networkCase.name} ${networkCase.contentHash.slice(0, 8)}`,
  });

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
        No explanation is available for <span className="font-mono text-fg">{search.sel}</span>. The
        element is in the network but has not been scored in this run.
      </p>
    );

  const railTitle = search.sel === undefined ? "Ranked contingencies" : "Selection";

  return (
    <div
      className={`flex h-full flex-col ${asOf ? "outline outline-1 -outline-offset-1 outline-amber" : ""}`}
    >
      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1 overflow-hidden bg-stage">
          <Scene model={model} routes={geometry.routes} colorOf={spanColor} onSelect={pick} />

          <GlassPanel
            title="Layers"
            meta={layerSummary(layers)}
            className="left-3 top-3 w-[264px] max-md:hidden"
          >
            <LayerStackPanel
              layers={layers}
              onToggle={(id) =>
                void (
                  (id === "ampacity" || id === "loading") &&
                  navigate({ search: (prev) => ({ ...prev, layer: id }) })
                )
              }
            />
          </GlassPanel>

          {/* The legend prints the unit and the breaks it is quantised to.
              It used to print `P(fail | forecast)` over a `haz-gbst v0.4.2`
              model line, and neither exists: `risk` is phase 6 and the
              platform publishes no probability at all. */}
          <GlassPanel
            title={`Encoding — ${encoding.label.toLowerCase()}`}
            className="bottom-10 left-3 w-[228px] max-md:hidden"
          >
            <div className="p-2.5">
              <MeasureRamp
                label={encoding.label}
                unit={encoding.unit}
                breaks={encoding.breaks}
                severity={encoding.severity}
              />
              <div className="mt-2.5 border-t border-line-2 pt-2 font-mono text-[9px] leading-relaxed text-fg-3">
                <span className="text-fg-2">cycle</span>{" "}
                {hazard.issuance === null ? "no pass has run" : `${hazard.issuance.slice(0, 16)}Z`}
                <br />
                <span className="text-fg-2">spans</span>{" "}
                {hazard.isPending
                  ? "loading…"
                  : `${hazard.rated} rated · ${hazard.unrated} unrated`}
                <br />
                <span className="text-fg-2">routes</span>{" "}
                {geometry.isPending
                  ? "loading…"
                  : geometry.error !== null
                    ? "unavailable"
                    : `${geometry.fetched} drawn${
                        geometry.fetched < geometry.available ? ` of ${geometry.available}` : ""
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
                  <span className="ml-auto">N-1 · f+{String(search.lead).padStart(2, "0")}</span>
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
