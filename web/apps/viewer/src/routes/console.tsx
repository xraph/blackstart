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
  ElementInspector,
  GlassPanel,
  LayerStackPanel,
  MeasureRamp,
  RAIL_BREAKPOINT,
  ScreeningRail,
  Select,
  SideRail,
  TimeAxis,
  Transport,
  layerSummary,
  useMediaQuery,
  type SelectOption,
} from "@blackstart/ui";

import { consoleRoute } from "../router";
import { temporalOf } from "../search/params";
import { useViewerModel } from "../model/use-viewer-model";
import { useRouteGeometry } from "../api/use-route-geometry";
import { useHazardLayer } from "../api/use-hazard-layer";
import { useSelectedCase } from "../api/use-context-options";
import { useScreening } from "../api/use-screening";
import { AMPACITY, LOADING, colorOf } from "../api/hazard";
import { hazardLayerStack } from "./layers";
import { HORIZON, INSPECTED, LINE_TULIA_LEVELLAND } from "../fixtures/derecho";

/* Each option states the order it produces, not the operation it performs.
 * "Reverse" and "expected cost ▾" both left the reader to work out what they
 * would be looking at afterwards. */
const SORTS: readonly SelectOption<"loading" | "violations" | "branch">[] = [
  { value: "loading", label: "worst loading" },
  { value: "violations", label: "most violations" },
  { value: "branch", label: "branch order" },
];

/* The filter is server-side and the sort is not, which is why they are two
 * controls rather than one: `concerning` is a smaller request, and a rail that
 * showed a subset without saying so would be the truncation the route
 * geometry already reports rather than hides. */
const SCOPES: readonly SelectOption<"concerning" | "all">[] = [
  { value: "concerning", label: "concerning" },
  { value: "all", label: "all screened" },
];

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

  const screening = useScreening(networkCase?.contentHash, search.sort, search.scope === "all");

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
    search.sel !== undefined ? (
      <p className="p-3 text-[11px] leading-snug text-fg-2">
        No explanation is available for <span className="font-mono text-fg">{search.sel}</span>. The
        element is in the network but has not been scored in this run.
      </p>
    ) : screening.gap !== null ? (
      /* Which step came up empty, not a spinner. "No case is selected" and
       * "this network has never been screened" are different problems with
       * different answers, and the same emptiness for both sends the reader
       * looking in the wrong place. */
      <p className="p-3 text-[11px] leading-snug text-fg-2">
        {screening.gap === "no-case"
          ? "Choose a case in the top bar to see what an N-1 screen made of it."
          : "This network has no recorded contingency run. Nothing has screened it yet."}
      </p>
    ) : (
      <ScreeningRail rows={screening.rows} />
    );

  const railTitle = search.sel === undefined ? "N-1 screening" : "Selection";

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
                /* The worst row, which is the one a reader opening the sheet
                   is looking for. It says nothing when there is nothing —
                   a peek naming a row that is not under it is worse than a
                   blank one. */
                <span className="font-mono text-[10px] tabular text-fg-2">
                  {screening.gap !== null || screening.rows.length === 0
                    ? "nothing screened"
                    : `branch ${screening.rows[0]?.branch} · ${screening.rows.length} shown`}
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
              count={
                screening.isPending
                  ? "…"
                  : /* The unevaluated count is printed even at zero. It is the
                       number that says whether the ranking can be trusted as
                       a ranking, and its absence would read as "none" only to
                       a reader who already knew to look. */
                    `${screening.rows.length} · ${screening.unevaluated} unevaluated`
              }
              subtitle={
                <>
                  <Select
                    label="Sort the screening"
                    value={search.sort}
                    options={SORTS}
                    onChange={(sort) => void navigate({ search: (p) => ({ ...p, sort }) })}
                  />
                  <Select
                    label="How much of the screening to show"
                    value={search.scope}
                    options={SCOPES}
                    onChange={(scope) => void navigate({ search: (p) => ({ ...p, scope }) })}
                  />
                  <span className="ml-auto">f+{String(search.lead).padStart(2, "0")}</span>
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
