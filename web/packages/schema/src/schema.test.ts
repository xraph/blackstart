/**
 * The invariants are the point of this package, so they are what is tested.
 *
 * Shape validation is Zod's job and testing it would be testing Zod. What is
 * worth a test is every rule this package adds on top: the ones that encode a
 * design decision, and would otherwise survive only as a comment.
 */

import { describe, expect, it } from "vitest";
import {
  ContingencyList,
  RiskDecomposition,
  rankByExpectedCost,
  riskBucket,
  probability,
  RISK_BREAKS,
} from "./risk";
import { CascadeRun } from "./cascade";
import { ScenarioRun, formatWall, compareRuns } from "./run";
import { chainCompleteness, ProvenanceChain } from "./provenance";
import { ValidTime, KnowledgeTime, temporalMode, leaksFuture, formatInstant } from "./temporal";
import { typeId, abbreviateTypeId, prefixOf } from "./identity";
import { formatQuantity, mw, hz, pu, mps, toSi, sameDimension, km, metres } from "./quantity";

const LN_RAW = "ln_01k3qf8m2xw7rb4n09ytvdhjkm";
const RUN = "run_01k3qf8m2xw7rb4n09ytvdhjkm";

/**
 * Parsed rather than asserted. A raw string does not satisfy `TypeId<"ln">`,
 * which is the brand working — fixtures earn their types the same way a
 * response from the platform does.
 */
const LN = typeId("ln").parse(LN_RAW);

const chain = ProvenanceChain.parse([
  { kind: "dataset", label: "hrrr.conus.2026080218z.f04", ref: "f04", verified: true },
  { kind: "run", label: "cascade", ref: RUN, verified: true },
  { kind: "commit", label: "blackstart", ref: "41416a7", verified: true },
]);

/** U+2009. Spelled with an escape so a reformat cannot silently widen it. */
const THIN = " ";

describe("quantities", () => {
  it("groups thousands with a thin space and uses a real minus sign", () => {
    expect(formatQuantity(mw(1284610))).toEqual({ value: `1${THIN}284${THIN}610`, unit: "MW" });
    expect(formatQuantity(mw(-318)).value).toBe("−318");
  });

  it("applies each unit's own display precision", () => {
    expect(formatQuantity(hz(59.6234)).value).toBe("59.62");
    expect(formatQuantity(mw(4187.4)).value).toBe(`4${THIN}187`);
    expect(formatQuantity(mps(31.74)).value).toBe("31.7");
  });

  it("refuses to render a per-unit value with no base", () => {
    expect(formatQuantity(pu(0.88)).value).toBe("—");
    expect(formatQuantity(pu(0.88, mw(100))).value).toBe("0.880");
  });

  it("converts to SI so different units of one dimension can be compared", () => {
    expect(toSi(km(87.4))).toBeCloseTo(87400);
    expect(sameDimension(km(1), metres(1))).toBe(true);
    expect(sameDimension(km(1), mw(1))).toBe(false);
  });
});

describe("risk buckets", () => {
  it("puts each break at the bottom of its band", () => {
    expect(riskBucket(0)).toBe(0);
    expect(riskBucket(RISK_BREAKS[0])).toBe(1);
    expect(riskBucket(0.049)).toBe(1);
    expect(riskBucket(0.05)).toBe(2);
    expect(riskBucket(0.341)).toBe(4);
    expect(riskBucket(1)).toBe(4);
  });
});

describe("risk decomposition", () => {
  const base = {
    baseline: 0.008,
    model: "haz-gbst v0.4.2",
    contributions: [
      { feature: "wind loading", delta: 0.164 },
      { feature: "vegetation", delta: 0.077 },
      { feature: "structure age", delta: 0.056 },
      { feature: "terrain exposure", delta: 0.039 },
      { feature: "recent inspection", delta: -0.003 },
    ],
  };

  it("accepts a waterfall that reconciles", () => {
    expect(() => RiskDecomposition.parse({ ...base, total: 0.341 })).not.toThrow();
  });

  it("rejects one that does not add up to its stated total", () => {
    const bad = RiskDecomposition.safeParse({ ...base, total: 0.5 });
    expect(bad.success).toBe(false);
    expect(bad.error?.issues[0]?.message).toContain("does not reconcile");
  });
});

describe("contingency ranking", () => {
  const row = (rank: number, eue: number) => ({
    rank,
    element: LN,
    name: "TULIA — LEVELLAND",
    voltage: { value: 345, unit: "kV" as const },
    length: { value: 87.4, unit: "km" as const },
    probability: probability(0.341),
    expectedUnservedEnergy: { value: eue, unit: "MW·h" as const },
    horizon: [0.05, 0.2, 0.341].map(probability),
  });

  it("accepts a list ordered by expected cost", () => {
    expect(ContingencyList.safeParse([row(1, 41.2), row(2, 34.8), row(3, 28.1)]).success).toBe(true);
  });

  it("rejects a list ordered by probability instead of cost", () => {
    const bad = ContingencyList.safeParse([row(1, 24.6), row(2, 41.2)]);
    expect(bad.success).toBe(false);
    expect(bad.error?.issues[0]?.message).toContain("ranked by expected cost");
  });

  it("rejects ranks that skip", () => {
    const bad = ContingencyList.safeParse([row(1, 41.2), row(3, 34.8)]);
    expect(bad.success).toBe(false);
    expect(bad.error?.issues[0]?.message).toContain("contiguous");
  });

  it("rankByExpectedCost produces a list the schema accepts", () => {
    const unordered = [row(0, 28.1), row(0, 41.2), row(0, 34.8)].map(({ rank: _r, ...rest }) => rest);
    expect(ContingencyList.safeParse(rankByExpectedCost(unordered)).success).toBe(true);
  });
});

describe("cascade runs", () => {
  const island = (id: number, state: "stable" | "collapsed") => ({
    id,
    balance: { value: -318, unit: "MW" as const },
    frequency: { value: 59.62, unit: "Hz" as const },
    nadir: { value: 59.4, unit: "Hz" as const },
    state,
    busCount: 22,
  });
  const base = {
    provenance: chain,
    run: RUN,
    initiating: "ln TULIA–LEVELLAND 345",
    islands: [island(1, "stable"), island(2, "collapsed")],
    customersInterrupted: { value: 1284610, unit: "customers" as const },
    loadLost: { value: 4187, unit: "MW" as const },
    expectedUnservedEnergy: { value: 12940, unit: "MW·h" as const },
    blastRadius: { value: 38400, unit: "km²" as const },
    countiesAffected: 31,
  };

  it("accepts events ordered in simulated time", () => {
    const ok = CascadeRun.safeParse({
      ...base,
      events: [
        { t: 0, kind: "trip", element: "ln TULIA–LEVELLAND 345", detail: "initiating" },
        { t: 4.21, kind: "island", element: "22 buses", detail: "−318 MW", island: 2 },
      ],
    });
    expect(ok.success).toBe(true);
  });

  it("rejects events that go backwards in time", () => {
    const bad = CascadeRun.safeParse({
      ...base,
      events: [
        { t: 4.21, kind: "trip", element: "a", detail: "x" },
        { t: 0.34, kind: "trip", element: "b", detail: "y" },
      ],
    });
    expect(bad.success).toBe(false);
    expect(bad.error?.issues[0]?.message).toContain("ordered in simulated time");
  });

  it("rejects an event referencing an island the run never formed", () => {
    const bad = CascadeRun.safeParse({
      ...base,
      events: [{ t: 1, kind: "shed", element: "UFLS block 6", detail: "214 MW", island: 7 }],
    });
    expect(bad.success).toBe(false);
    expect(bad.error?.issues[0]?.message).toContain("never formed");
  });
});

describe("scenario runs", () => {
  const base = {
    id: RUN,
    scenario: "derecho · S Plains",
    temporal: {
      validTime: "2026-08-02T22:00Z",
      knowledgeTime: "2026-08-02T18:14Z",
    },
    solverVersion: "0.3.1",
    modelVersion: "0.4.2",
    contingenciesSolved: 142,
    wallMs: 258000,
  };

  it("requires a complete run to report a result", () => {
    const bad = ScenarioRun.safeParse({
      ...base,
      status: "complete",
      expectedUnservedEnergy: null,
      repro: "bit-exact",
    });
    expect(bad.success).toBe(false);
    expect(bad.error?.issues[0]?.message).toContain("must report expected unserved energy");
  });

  it("refuses a reproducibility verdict on a run that has not finished", () => {
    const bad = ScenarioRun.safeParse({
      ...base,
      status: "running",
      expectedUnservedEnergy: null,
      repro: "bit-exact",
    });
    expect(bad.success).toBe(false);
    expect(bad.error?.issues[0]?.message).toContain("cannot be verdicted");
  });

  it("only calls two runs comparable when both time axes match", () => {
    const a = ScenarioRun.parse({
      ...base,
      status: "complete",
      expectedUnservedEnergy: { value: 12940, unit: "MW·h" },
      repro: "bit-exact",
    });
    const b = ScenarioRun.parse({
      ...base,
      id: "run_01k3m2r7vcw7rb4n09ytvdhjkm",
      status: "complete",
      expectedUnservedEnergy: { value: 10800, unit: "MW·h" },
      repro: "bit-exact",
    });
    const cmp = compareRuns(a, b);
    expect(cmp.comparable).toBe(true);
    expect(cmp.deltaEue).toBe(-2140);

    const shifted = ScenarioRun.parse({
      ...b,
      temporal: { ...base.temporal, knowledgeTime: "2026-07-15T18:00Z" },
    });
    expect(compareRuns(a, shifted).comparable).toBe(false);
  });

  it("formats wall time the way the grid prints it", () => {
    expect(formatWall(258000)).toBe("4m 18s");
    expect(formatWall(null)).toBe("—");
  });
});

describe("provenance", () => {
  it("computes completeness rather than trusting a flag", () => {
    expect(chainCompleteness(chain).complete).toBe(true);

    const partial = ProvenanceChain.parse(chain.slice(0, 2));
    const c = chainCompleteness(partial);
    expect(c.complete).toBe(false);
    expect(c.missing).toContain("commit");
  });

  it("counts a present-but-unverified link as incomplete", () => {
    const unverified = ProvenanceChain.parse([
      ...chain.slice(0, 2),
      { kind: "commit", label: "blackstart", ref: "41416a7", verified: false },
    ]);
    const c = chainCompleteness(unverified);
    expect(c.complete).toBe(false);
    expect(c.unverified).toContain("commit");
  });

  it("rejects two links of the same kind", () => {
    expect(ProvenanceChain.safeParse([...chain, chain[0]]).success).toBe(false);
  });
});

describe("temporal", () => {
  const now = new Date("2026-08-02T18:20:00Z");

  it("requires UTC instants", () => {
    expect(ValidTime.safeParse("2026-08-02T22:00Z").success).toBe(true);
    expect(ValidTime.safeParse("2026-08-02T22:00+02:00").success).toBe(false);
    expect(ValidTime.safeParse("2026-08-02 22:00").success).toBe(false);
  });

  it("classifies a knowledge time well behind now as an as-of view", () => {
    const live = {
      validTime: ValidTime.parse("2026-08-02T22:00Z"),
      knowledgeTime: KnowledgeTime.parse("2026-08-02T18:14Z"),
    };
    const backtest = {
      validTime: ValidTime.parse("2019-05-18T03:00Z"),
      knowledgeTime: KnowledgeTime.parse("2019-05-18T00:00Z"),
    };
    expect(temporalMode(live, now)).toBe("live");
    expect(temporalMode(backtest, now)).toBe("as-of");
  });

  it("detects a knowledge time that postdates what it is forecasting", () => {
    expect(
      leaksFuture({
        validTime: ValidTime.parse("2019-05-18T03:00Z"),
        knowledgeTime: KnowledgeTime.parse("2019-05-25T00:00Z"),
      }),
    ).toBe(true);
  });

  it("prints instants the way the console does", () => {
    expect(formatInstant("2026-08-02T22:00Z")).toBe("2026-08-02 22:00Z");
  });
});

describe("identity", () => {
  it("keeps prefixes apart", () => {
    expect(typeId("ln").safeParse(LN_RAW).success).toBe(true);
    expect(typeId("run").safeParse(LN_RAW).success).toBe(false);
  });

  it("rejects the base32 characters Crockford excludes", () => {
    expect(typeId("ln").safeParse("ln_01k3qf8m2xw7rb4n09ytvdhjki").success).toBe(false);
  });

  it("abbreviates from the tail, since the head is a shared timestamp", () => {
    expect(abbreviateTypeId(LN)).toBe("ln_…vdhjkm");
    expect(abbreviateTypeId(LN, 4)).toBe("ln_…hjkm");
    expect(prefixOf(LN)).toBe("ln");
    expect(prefixOf("not-an-id")).toBe(null);
  });
});

describe("probability brand", () => {
  it("rejects values outside the unit interval", () => {
    expect(() => probability(1.4)).toThrow();
    expect(probability(0.341)).toBe(0.341);
  });
});
