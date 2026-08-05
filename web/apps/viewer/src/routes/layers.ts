/**
 * The layer stack, built from what the platform actually answered.
 *
 * It was a fixture, and five of its seven rows described Phase 7 work that no
 * build can draw. The rows stay — the stack shows the intended shape of the
 * system as well as its current state, and a panel that quietly dropped them
 * would make the roadmap invisible in the product — but they are marked, so
 * nobody clicks a control that cannot do anything.
 *
 * The three real rows carry the dataset versions they are really drawing.
 * That is what makes a screenshot self-describing: a figure in a review can be
 * traced back to a forecast cycle without asking who took it and when.
 */

import { LayerStack, type Layer } from "@blackstart/schema";

import { AMPACITY, LOADING, type Encoding } from "../api/hazard";

export interface HazardStackState {
  /** The quantity currently drawn. The other hazard row is present but off. */
  readonly encoding: Encoding;
  /** The cycle issuance the values came from, or null if no pass has run. */
  readonly issuance: string | null;
  readonly rated: number;
  readonly unrated: number;
  /** The network model version, as the network row reports it. */
  readonly network: string;
}

/** Phase 7. Listed so the panel shows the shape of the system, not drawable. */
const ABSENT: readonly (readonly [string, string, string])[] = [
  ["clouds", "Clouds", "HRRR · phase 7"],
  ["wind", "Wind field", "HRRR gust · ph 7"],
  ["precip", "Precip / hail", "MRMS · phase 7"],
  ["terrain", "Terrain", "3DEP · phase 7"],
  ["veg", "Vegetation", "LANDFIRE · ph 7"],
];

/**
 * What a hazard row says about where its numbers came from.
 *
 * A pass that never ran and a pass that rated nothing are different failures —
 * one is a missing job, the other a missing conductor assignment — and the row
 * is the only place a reader finds out which.
 */
function hazardSource(issuance: string | null, rated: number): string {
  if (issuance === null) return "no pass has run";

  /* Month, day and hour, not the full instant. The panel gives the source
   * about eighteen monospace characters and the row is a fixed 30px, so a
   * longer string wraps and overlaps its neighbours — which is exactly what
   * the full ISO instant did. Cycles are hourly, so this still names one. */
  return `${issuance.slice(5, 10)} ${issuance.slice(11, 13)}Z · ${rated}`;
}

export function hazardLayerStack(state: HazardStackState): LayerStack {
  const hazardRow = (encoding: Encoding): Layer => {
    const on = encoding.id === state.encoding.id;

    return {
      id: encoding.id,
      label: encoding.label,
      state: on ? "on" : "off",
      opacity: on ? 1 : 0,
      source: hazardSource(state.issuance, state.rated),
    };
  };

  return LayerStack.parse([
    { id: "network", label: "Network", state: "on", opacity: 1, source: state.network },
    hazardRow(AMPACITY),
    hazardRow(LOADING),
    ...ABSENT.map(([id, label, source]) => ({
      id,
      label,
      state: "unavailable" as const,
      opacity: 0,
      source,
    })),
  ]);
}
