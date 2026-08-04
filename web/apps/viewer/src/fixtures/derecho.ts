/**
 * The South Plains derecho, as reviewed.
 *
 * Every export is produced by `.parse()` rather than asserted with `as`, so
 * the fixture cannot drift from the schema: a violated invariant throws at
 * import time and names the field. That makes this both sample data and a
 * standing test of whether the schemas accept realistic input — which is the
 * part a hand-written mock object would quietly skip.
 *
 * Synthetic ACTIVSg2000 network throughout. No real utility topology is
 * depicted, which is the condition under which this analysis can be done in
 * the open at all.
 */

import {
  CascadeRun,
  ContingencyList,
  ElementExplanation,
  ForecastHorizon,
  LayerStack,
  ProvenanceChain,
  ScenarioRun,
  amps,
  customers,
  degC,
  hz,
  km,
  km2,
  kv,
  mps,
  mw,
  mwh,
  probability,
  rankByExpectedCost,
  type Contingency,
  type Probability,
} from "@blackstart/schema";

/* ── identifiers ──────────────────────────────────────────────────────
 * TypeID suffixes are Crockford base32: digits plus lowercase letters less
 * i, l, o and u. Written out rather than generated so the fixture is stable
 * across runs — a citable view must resolve to the same element tomorrow. */

/* Both stems are twenty characters, so a six-character tail completes the
 * twenty-six a TypeID suffix requires. */
const LN = (tail: string): string => `ln_01k3qf8m2xw7rb4n09yt${tail}`;
const RUN = (tail: string): string => `run_01k3qf8m2xw7rb4n09yv${tail}`;
/** Same suffix scheme as `RUN`, under the prefix the engine mints for a cascade. */
const CASC = (tail: string): string => `casc_01k3qf8m2xw7rb4n09yv${tail}`;

export const LINE_TULIA_LEVELLAND = LN("vdhjkm");

const PROVENANCE = ProvenanceChain.parse([
  {
    kind: "dataset",
    label: "hrrr.conus.2026080218z.f04",
    ref: "f04",
    digest: "sha256 4f9c…a17b",
    verified: true,
  },
  { kind: "network", label: "activsg2000@v3", ref: "tx_01k2…", verified: true },
  { kind: "feature-set", label: "fs-span-exposure v11", ref: "v11", verified: true },
  { kind: "model", label: "haz-gbst 0.4.2", ref: "mdl_01k3p7…", verified: true },
  { kind: "run", label: "run_01k3qf8m2x", ref: "seed 1 099 511", verified: true },
  { kind: "commit", label: "41416a7 · reproducible", ref: "41416a7", verified: true },
]);

export const HORIZON = ForecastHorizon.parse({
  cycle: "HRRR 2026-08-02 18Z",
  issued: "2026-08-02T18:52Z",
  leadHours: Array.from({ length: 19 }, (_, i) => i),
});

export const LAYERS = LayerStack.parse([
  { id: "network", label: "Network", state: "on", opacity: 1, source: "ACTIVSg2000 v3" },
  { id: "risk", label: "Risk surface", state: "on", opacity: 0.82, source: "haz-2026.08.02-18Z" },
  { id: "clouds", label: "Clouds", state: "on", opacity: 0.64, source: "HRRR conus 18Z f04" },
  { id: "wind", label: "Wind field", state: "partial", opacity: 0.4, source: "HRRR 10 m gust" },
  { id: "precip", label: "Precip / hail", state: "off", opacity: 0, source: "MRMS 2 min" },
  { id: "terrain", label: "Terrain", state: "on", opacity: 1, source: "3DEP 10 m · 3D Tiles" },
  { id: "veg", label: "Vegetation", state: "off", opacity: 0, source: "LANDFIRE 2024" },
]);

/* ── ranked contingencies ─────────────────────────────────────────────
 * Ranks are assigned by rankByExpectedCost rather than written down, so the
 * ordering invariant is produced by the same function the application would
 * use. Note rank 4 is likelier than rank 3 — that inversion is the point of
 * ranking by cost, and the fixture would be misleading without one. */

/** Risk accumulating over the forecast horizon, ending at the stated value. */
function ramp(p: number): Probability[] {
  return [0.06, 0.18, 0.42, 0.74, 1].map((f) => probability(Number((p * f).toFixed(4))));
}

const UNRANKED: Omit<Contingency, "rank">[] = (
  [
    ["vdhjkm", "TULIA — LEVELLAND", 345, 87.4, 0.341, 41.2],
    ["vdhjkn", "PLAINVIEW — HALE CO", 345, 41.0, 0.298, 34.8],
    ["vdhjkp", "AMARILLO S — PAMPA", 230, 96.2, 0.264, 28.1],
    ["vdhjkq", "LUBBOCK N — IDALOU", 115, 22.7, 0.312, 24.6],
    ["vdhjkr", "CHILDRESS — VERNON", 230, 84.3, 0.207, 21.9],
    ["vdhjks", "TULIA — PLAINVIEW", 345, 40.1, 0.281, 19.4],
    ["vdhjkt", "LEVELLAND — ANDREWS", 230, 141.8, 0.164, 16.2],
    ["vdhjkv", "SNYDER — SWEETWATER", 345, 52.6, 0.118, 13.7],
    ["vdhjkw", "VERNON — WICHITA F", 345, 74.9, 0.096, 11.8],
    ["vdhjkx", "BIG SPRING — MIDLAND", 230, 63.4, 0.081, 9.4],
    ["vdhjky", "SEYMOUR — GRAHAM", 115, 69.7, 0.074, 7.1],
    ["vdhjkz", "ABILENE — SNYDER", 345, 108.2, 0.052, 6.3],
  ] as const
).map(([tail, name, volts, length, p, eue]) => ({
  element: LN(tail) as Contingency["element"],
  name,
  voltage: kv(volts),
  length: km(length),
  probability: probability(p),
  expectedUnservedEnergy: mwh(eue),
  horizon: ramp(p),
}));

export const CONTINGENCIES = ContingencyList.parse(rankByExpectedCost(UNRANKED));

/* ── the cascade ──────────────────────────────────────────────────────
 * Events ascend in simulated time, which the schema enforces, and every
 * island referenced below is one the run actually formed — the other
 * invariant. Both exist because breaking either produces a plausible
 * animation of something that did not happen. */

export const CASCADE = CascadeRun.parse({
  provenance: PROVENANCE,
  run: CASC("tvdhjk"),
  initiating: "ln TULIA–LEVELLAND 345 kV",
  events: [
    { t: 0, kind: "trip", element: "ln TULIA–LEVELLAND 345", detail: "INITIATING" },
    { t: 0.34, kind: "trip", element: "bkr TULIA 345 A1/A2", detail: "protection op" },
    { t: 1.18, kind: "overload", element: "ln SNYDER–SWEETWATER 345", detail: "108% · watch" },
    { t: 2.71, kind: "overload", element: "ln PLAINVIEW–HALE CO 345", detail: "134% · 12 s" },
    { t: 2.94, kind: "trip", element: "ln PLAINVIEW–HALE CO 345", detail: "overload" },
    { t: 3.86, kind: "overload", element: "ln LEVELLAND–ANDREWS 230", detail: "149% · 4 s", island: 2 },
    { t: 4.12, kind: "trip", element: "ln LEVELLAND–ANDREWS 230", detail: "dist zone 2", island: 2 },
    { t: 4.21, kind: "island", element: "22 buses separate", detail: "−318 MW", island: 2 },
    { t: 4.27, kind: "overload", element: "ln TULIA–PLAINVIEW 345", detail: "128% · 6 s", island: 3 },
    { t: 4.33, kind: "overload", element: "ln LUBBOCK N–IDALOU 115", detail: "163% · 2 s", island: 3 },
    { t: 4.41, kind: "trip", element: "ln TULIA–PLAINVIEW 345", detail: "dist zone 1", island: 3 },
    { t: 4.55, kind: "trip", element: "ln LUBBOCK N–IDALOU 115", detail: "overload", island: 3 },
    { t: 4.63, kind: "island", element: "14 buses separate", detail: "−96 MW", island: 3 },
    { t: 4.71, kind: "shed", element: "UVLS block 3 · 118 MW", detail: "0.88 pu", island: 2 },
    { t: 4.88, kind: "overload", element: "ln CHILDRESS–VERNON 230", detail: "141% · 8 s", island: 4 },
    { t: 5.02, kind: "trip", element: "ln AMARILLO S–PAMPA 230", detail: "dist zone 2", island: 4 },
    { t: 5.19, kind: "island", element: "9 buses separate", detail: "−1 244 MW", island: 4 },
    { t: 5.44, kind: "trip", element: "gen PLAINVIEW CT-2", detail: "underfrequency", island: 4 },
    { t: 5.71, kind: "shed", element: "UFLS block 5 · 268 MW", detail: "59.3 Hz", island: 2 },
    { t: 6.02, kind: "shed", element: "UFLS block 6 · 214 MW", detail: "59.4 Hz", island: 4 },
    { t: 6.44, kind: "stabilise", element: "Island 1 settles", detail: "60.01 Hz · Δf 0.09", island: 1 },
  ],
  islands: [
    { id: 1, balance: mw(842), frequency: hz(60.01), nadir: hz(59.94), state: "stable", busCount: 61 },
    { id: 2, balance: mw(-318), frequency: hz(59.62), nadir: hz(59.4), state: "shedding", busCount: 22 },
    { id: 3, balance: mw(-96), frequency: hz(59.88), nadir: hz(59.71), state: "recovering", busCount: 14 },
    { id: 4, balance: mw(-1244), frequency: hz(57.41), nadir: hz(57.41), state: "collapsed", busCount: 9 },
  ],
  customersInterrupted: customers(1284610),
  loadLost: mw(4187),
  expectedUnservedEnergy: mwh(12940),
  blastRadius: km2(38400),
  countiesAffected: 31,
});

/**
 * Frequency samples per island for the counter strip's small multiples.
 *
 * Generated from the nadir rather than written out: forty-four hand-typed
 * decimals per island would be unreadable and impossible to check, and the
 * shape — a slow decline, a dip to the nadir, then recovery or collapse — is
 * what the trace is actually communicating.
 */
const NOMINAL_HZ = 60;
/** Frequency has already sagged this far by the time separation begins. */
const PRE_DROOP = 0.06;

function trace(nadir: number, recovers: boolean): number[] {
  const drop = NOMINAL_HZ - nadir - PRE_DROOP;
  return Array.from({ length: 44 }, (_, i) => {
    const t = i / 43;
    if (t < 0.28) return round(NOMINAL_HZ - (t / 0.28) * PRE_DROOP);
    const u = (t - 0.28) / 0.72;

    // A collapsing island does not turn around: it falls to its nadir and
    // stays there, so the minimum is the last sample rather than a dip.
    if (!recovers) return round(NOMINAL_HZ - PRE_DROOP - drop * Math.pow(u, 1.4));

    const dip = drop * Math.exp(-Math.pow((u - 0.34) / 0.26, 2));
    const back = drop * 0.55 * Math.min(1, Math.max(0, (u - 0.5) / 0.5));
    return round(NOMINAL_HZ - PRE_DROOP - dip + back);
  });
}

const round = (hz: number): number => Number(hz.toFixed(3));

/**
 * Derived from the islands rather than listed alongside them.
 *
 * Written out separately, a trace and the nadir it is supposed to reach drift
 * the moment one is edited — which is exactly what happened on the first
 * attempt here, where the settled frequency was passed instead of the nadir
 * and the chart quietly disagreed with the number printed beside it.
 */
export const FREQ_TRACES = new Map<number, number[]>(
  CASCADE.islands
    // An island with no machine attached has no nadir to trace — absent, not
    // zero. Filtered rather than defaulted, because trace(0) would draw a
    // collapse to 0 Hz that the run never reported.
    .filter((i) => i.state !== "stable" && i.nadir !== undefined)
    .map((i) => [i.id, trace(i.nadir!.value, i.state !== "collapsed")]),
);

/* ── the inspected element ────────────────────────────────────────── */

export const INSPECTED = ElementExplanation.parse({
  element: {
    provenance: PROVENANCE,
    id: LINE_TULIA_LEVELLAND,
    name: "TULIA — LEVELLAND 345 kV",
    voltage: kv(345),
    length: km(87.4),
    spanCount: 214,
    structureCount: 213,
    conductor: 'ACSR 954 kcmil "Rail"',
    bundle: 2,
    rating: { normal: amps(1194), emergency: amps(1433), ambient: degC(40) },
    aliases: [
      { system: "ACTIVSg2000", externalId: "LINE-4471", authority: "topology" },
      { system: "HIFLD", externalId: "TL-482913", authority: "geometry" },
      { system: "OSM", externalId: "way/318827441", authority: "route" },
    ],
  },
  probability: probability(0.341),
  rank: 1,
  totalRanked: 142,
  decomposition: {
    baseline: 0.008,
    model: "haz-gbst v0.4.2",
    total: probability(0.341),
    contributions: [
      { feature: "wind loading", delta: 0.164, evidence: "gust 31.7 m·s⁻¹" },
      { feature: "vegetation", delta: 0.077, evidence: "clearance 4.1 m" },
      { feature: "structure age", delta: 0.056, evidence: "installed 2001" },
      { feature: "terrain exposure", delta: 0.039, evidence: "ridge crossing, spans 61–98" },
      { feature: "recent inspection", delta: -0.003, evidence: "2026-03, no findings" },
    ],
  },
  conditions: [
    { label: "GUST 10 M", current: { value: 31.7, unit: "m·s⁻¹" }, points: [6, 7, 9, 12, 16, 21, 27, 31.7], bucket: 4 },
    { label: "CONDUCTOR T", current: { value: 61, unit: "°C" }, points: [38, 40, 43, 47, 52, 56, 59, 61], bucket: 3 },
    { label: "VEG CLEAR", current: { value: 4.1, unit: "m" }, points: [7.2, 7, 6.6, 6, 5.4, 4.9, 4.4, 4.1], bucket: 2 },
    { label: "LOADING", current: { value: 71, unit: "%" }, points: [44, 47, 51, 56, 61, 65, 69, 71], bucket: 1 },
  ],
  analogues: [
    { date: "2015-12-26", similarity: 0.91, event: "Panhandle wind event", outcome: "4 structures lost" },
    { date: "2019-05-18", similarity: 0.86, event: "South Plains derecho", outcome: "conductor slap, 2 trips" },
    { date: "2023-06-14", similarity: 0.79, event: "Matador supercell", outcome: "held · veg contact only" },
  ],
  counterfactual: {
    feature: "peak gust along spans 61–98",
    current: mps(31.7),
    threshold: mps(24.1),
    wouldFallTo: probability(0.049),
    requiresMultiple: true,
  },
});

/* ── the runs index ───────────────────────────────────────────────────
 * The schema refuses a result on a run that has not completed and refuses a
 * reproducibility verdict on one still in flight, so the incomplete rows here
 * carry null and "pending" because they must, not by convention. */

type RunSeed = readonly [
  tail: string,
  scenario: string,
  valid: string | null,
  known: string,
  solver: string,
  model: string | null,
  status: "queued" | "running" | "complete" | "failed",
  contingencies: number,
  eue: number | null,
  wallMs: number | null,
  repro: "bit-exact" | "drift" | "pending",
];

const RUN_SEEDS: readonly RunSeed[] = [
  ["tvdhjk", "derecho · S Plains", "2026-08-02T22:00Z", "2026-08-02T18:14Z", "0.3.1", "0.4.2", "complete", 142, 12940, 258000, "bit-exact"],
  ["tvdhjm", "derecho · no-veg", "2026-08-02T22:00Z", "2026-08-02T18:14Z", "0.3.1", "0.4.2", "complete", 142, 10800, 242000, "bit-exact"],
  ["tvdhjn", "derecho · f+06", "2026-08-03T00:00Z", "2026-08-02T18:14Z", "0.3.1", "0.4.2", "running", 96, null, 131000, "pending"],
  ["tvdhjp", "N-1 sweep · base", "2026-08-02T20:00Z", "2026-08-02T18:14Z", "0.3.1", "0.4.2", "complete", 2094, 412, 706000, "bit-exact"],
  ["tvdhjq", "derecho · seed 2", "2026-08-02T22:00Z", "2026-08-02T18:14Z", "0.3.1", "0.4.2", "complete", 142, 12938, 261000, "bit-exact"],
  ["tvdhjr", "BACKTEST 2019-05-18", "2019-05-18T03:00Z", "2019-05-18T00:00Z", "0.3.1", "0.4.2", "complete", 188, 9412, 333000, "bit-exact"],
  ["tvdhjs", "BACKTEST 2015-12-26", "2015-12-26T09:00Z", "2015-12-26T06:00Z", "0.3.1", "0.4.2", "complete", 164, 7188, 302000, "bit-exact"],
  ["tvdhjt", "ice · Hill Country", "2026-01-14T12:00Z", "2026-08-01T09:20Z", "0.3.0", "0.4.1", "complete", 210, 15602, 400000, "drift"],
  ["tvdhjv", "N-1-1 · West TX", "2026-08-01T18:00Z", "2026-08-01T17:55Z", "0.3.0", "0.4.1", "complete", 3880, 1044, 1689000, "bit-exact"],
  ["tvdhjw", "derecho · calib sweep", "2026-08-01T22:00Z", "2026-08-01T17:55Z", "0.3.0", "0.4.1", "failed", 0, null, 41000, "pending"],
  ["tvdhjx", "IEEE-300 validation", null, "2026-08-01T14:02Z", "0.3.0", null, "complete", 300, 0, 67000, "bit-exact"],
  ["tvdhjy", "IEEE-118 validation", null, "2026-08-01T14:00Z", "0.3.0", null, "complete", 118, 0, 22000, "bit-exact"],
  ["tvdhjz", "heat · ERCOT peak", "2026-07-28T21:00Z", "2026-07-28T16:40Z", "0.3.0", "0.4.1", "complete", 142, 3204, 295000, "bit-exact"],
  ["tvdhk0", "hurricane · Coastal Bend", "2026-07-24T06:00Z", "2026-07-23T22:15Z", "0.2.9", "0.4.0", "complete", 264, 28114, 552000, "drift"],
  ["tvdhk1", "hurricane · landfall +12", "2026-07-24T18:00Z", "2026-07-23T22:15Z", "0.2.9", "0.4.0", "complete", 264, 31980, 584000, "bit-exact"],
  ["tvdhk2", "N-1 sweep · summer", "2026-07-20T20:00Z", "2026-07-20T15:00Z", "0.2.9", "0.4.0", "complete", 2094, 388, 723000, "bit-exact"],
  ["tvdhk3", "wildfire · Panhandle", "2026-07-18T22:00Z", "2026-07-18T19:30Z", "0.2.9", "0.4.0", "complete", 142, 6620, 270000, "bit-exact"],
  ["tvdhk4", "derecho · ablation veg", "2026-07-15T22:00Z", "2026-07-15T18:00Z", "0.2.9", "0.3.8", "complete", 142, 11402, 252000, "bit-exact"],
  ["tvdhk5", "BACKTEST 2023-06-14", "2023-06-14T21:00Z", "2023-06-14T18:00Z", "0.2.9", "0.3.8", "queued", 0, null, null, "pending"],
];

export const RUNS: ScenarioRun[] = RUN_SEEDS.map(
  ([tail, scenario, valid, known, solver, model, status, cont, eue, wallMs, repro]) =>
    ScenarioRun.parse({
      id: RUN(tail),
      scenario,
      temporal: valid === null ? { knowledgeTime: known } : { validTime: valid, knowledgeTime: known },
      solverVersion: solver,
      modelVersion: model,
      status,
      contingenciesSolved: cont,
      expectedUnservedEnergy: eue === null ? null : mwh(eue),
      wallMs,
      repro,
    }),
);
