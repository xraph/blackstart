// @vitest-environment jsdom
/**
 * Cascade and runs, driven the way the URL drives them.
 *
 * The globe is stubbed: WebGL does not exist in jsdom, and what is under test
 * is composition and state, not the renderer.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { RouterProvider, createMemoryHistory, createRouter } from "@tanstack/react-router";

vi.mock("@blackstart/globe", async () => {
  const actual = await vi.importActual<typeof import("@blackstart/globe")>("@blackstart/globe");
  return { ...actual, Scene: () => <div data-testid="scene" /> };
});

/**
 * The runs and cascade screens fetch. Stubbing the generated hooks keeps these
 * tests about the screen's states — loading, error, empty, populated — rather
 * than about the transport, which has its own tests in the client package.
 */
const runsQuery = vi.hoisted(() => ({
  current: { data: undefined, isPending: true, isError: false, error: null } as unknown,
}));
const cascadeQuery = vi.hoisted(() => ({
  current: { data: undefined, isPending: true, isError: false, error: null } as unknown,
}));

vi.mock("@blackstart/api-client", async () => {
  const actual =
    await vi.importActual<typeof import("@blackstart/api-client")>("@blackstart/api-client");
  return {
    ...actual,
    useContingencyRunsList: () => runsQuery.current,
    useCascadeRunsGet: () => cascadeQuery.current,
  };
});

import { ApiProvider } from "../api/provider";
import { routeTree } from "../router";
import { CASCADE } from "../fixtures/derecho";

/**
 * The cascade screen fetches the wire shape, not `CascadeRun` — bare numbers,
 * an island `condition` instead of `state`, and no island id at all. Building
 * this from `CASCADE` rather than writing it out twice keeps the two fixtures
 * from drifting apart, and exercises the same wire-to-domain translation the
 * screen runs against the real platform.
 */
const WIRE_CASCADE = {
  id: CASCADE.run,
  modelHash: "9f2c1a7e44b0d3castille",
  solverVersion: "0.3.1",
  startedAt: "2026-08-02T22:00:00Z",
  finishedAt: "2026-08-02T22:04:18Z",
  elapsedS: 18.44,
  steps: 812,
  outcome: "collapsed",
  initiating: [CASCADE.initiating],
  loadLost: CASCADE.loadLost.value,
  events: CASCADE.events.map((e) => ({
    at: e.t,
    // The proto spells this "stabilize"; the schema spells it "stabilise".
    kind: e.kind === "stabilise" ? "stabilize" : e.kind,
    element: e.element,
    detail: e.detail,
    // `CASCADE.events[].island` is already the domain's 1-based id. The wire
    // is 0-based — island 0 is a real, first-formed island, not "no split
    // yet" (that's what an absent island means) — so this fixture has to
    // shift back down to look like the wire, or it never contains the 0 a
    // real payload would and the adapter's own +1 goes untested.
    island: e.island === undefined ? undefined : e.island - 1,
  })),
  islands: CASCADE.islands.map((i) => ({
    balance: i.balance.value,
    busCount: i.busCount,
    buses: [],
    condition: i.state,
    frequency: i.frequency?.value,
    nadir: i.nadir?.value,
    shed: 0,
    verdict: "secure",
    // No trace: this fixture is exercising the wire translation, not the
    // small-multiples chart, and an absent trace is itself a valid state.
  })),
};

/**
 * A run small enough that a search string appears exactly once. `WIRE_CASCADE`
 * above repeats "TULIA" across several events and the initiating line by
 * design — that redundancy is the point of showing an outage in two places —
 * which makes it the wrong fixture for a single-match `findByText`.
 *
 * It is also, unavoidably, a run with every figure the real engine never
 * computes absent: `customersInterrupted`, `expectedUnservedEnergy`,
 * `blastRadius` and `countiesAffected` do not exist on the wire at all, for
 * any run, which is exactly the condition the "not computed" test exists to
 * check against real translation rather than a fixture built to prove it.
 */
const MINIMAL_RUN = {
  id: CASCADE.run,
  modelHash: "9f2c1a7e44b0d3castille",
  solverVersion: "0.3.1",
  startedAt: "2026-08-02T22:00:00Z",
  finishedAt: "2026-08-02T22:00:12Z",
  elapsedS: 4.1,
  steps: 12,
  outcome: "collapsed",
  initiating: ["ln TULIA-LEVELLAND 345"],
  loadLost: 4187,
  events: [{ at: 0, kind: "trip", element: "bkr NORTH JCT 230 A1", detail: "protection op" }],
  islands: [
    { balance: 842, busCount: 61, buses: [], condition: "stable", verdict: "secure", shed: 0 },
  ],
};

/**
 * Wire island indices are 0-based, and island 0 is a real, first-formed
 * island — proto/blackstart/solver/v1/solver.proto is explicit that this is
 * distinct from an absent `island`, which means the split has not happened
 * yet. `MINIMAL_RUN` above never exercises this because it has no island
 * reference at all; this fixture's only event names island 0 on purpose.
 */
const ISLAND_ZERO_RUN = {
  id: CASCADE.run,
  modelHash: "9f2c1a7e44b0d3castille",
  solverVersion: "0.3.1",
  startedAt: "2026-08-02T22:00:00Z",
  finishedAt: "2026-08-02T22:00:05Z",
  elapsedS: 5.19,
  steps: 8,
  outcome: "collapsed",
  initiating: ["ln CHILDRESS-VERNON 230"],
  loadLost: 4187,
  events: [
    { at: 5.19, kind: "island", element: "9 buses separate", detail: "-1244 MW", island: 0 },
  ],
  islands: [
    {
      balance: -1244,
      busCount: 9,
      buses: [],
      condition: "collapsed",
      verdict: "violated",
      shed: 0,
    },
  ],
};

const SWEEP = {
  id: "run_01k3qf8m2xw7rb4n09yvtvdhjk",
  modelHash: "9f2c1a7e44b0d3castille",
  solverVersion: "0.3.1",
  startedAt: "2026-08-02T22:00:00Z",
  finishedAt: "2026-08-02T22:04:18Z",
  candidates: [
    {
      branch: 4471,
      contingencyId: "c-1",
      screenability: "screened",
      violations: [{ branch: 4471, loading: 1.18, pFlow: 412, rating: 350 }],
    },
  ],
};

const loading = { data: undefined, isPending: true, isError: false, error: null };
const failed = {
  data: undefined,
  isPending: false,
  isError: true,
  error: new Error("Failed to fetch"),
};
const loaded = (runs: unknown[]) => ({
  data: { runs },
  isPending: false,
  isError: false,
  error: null,
});
/** `cascadeRunsGet`-shaped: the payload *is* the run, not `{ runs }`. */
const loadedRun = (run: unknown) => ({
  data: run,
  isPending: false,
  isError: false,
  error: null,
});

function stubViewport(wide: boolean): void {
  vi.stubGlobal("matchMedia", (q: string) => ({
    matches: q.includes("min-width") ? wide : false,
    media: q,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
}

beforeEach(() => {
  stubViewport(true);
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.resolve({ ok: false, status: 404 } as Response)),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

/**
 * Waits for the initial route to resolve before rendering.
 *
 * Without `load()` the first paint is the router's pending state — an empty
 * div — and every assertion fails against a blank document rather than
 * against the screen.
 */
async function renderAt(url: string): Promise<void> {
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [url] }),
  });
  await router.load();
  render(
    <ApiProvider baseUrl="http://localhost:8080">
      <RouterProvider router={router} />
    </ApiProvider>,
  );
}

const VALID = "2026-08-02T22:00Z";
const RUN_URL = `/cascade?valid=${VALID}&run=${CASCADE.run}`;

describe("cascade screen", () => {
  beforeEach(() => {
    cascadeQuery.current = loadedRun(WIRE_CASCADE);
  });

  it("starts at the end of the run, showing every event", async () => {
    await renderAt(RUN_URL);
    await screen.findByTestId("scene");
    expect(screen.getByText(`${CASCADE.events.length} / ${CASCADE.events.length}`)).toBeTruthy();
  });

  it("shows the initiating outage, which is the row the reader looks for first", async () => {
    await renderAt(RUN_URL);
    await screen.findByTestId("scene");
    expect(screen.getByText("INITIATING")).toBeTruthy();
  });

  it("labels every island the run formed", async () => {
    await renderAt(RUN_URL);
    await screen.findByTestId("scene");
    for (const island of CASCADE.islands) {
      expect(screen.getAllByText(`Island ${island.id}`).length).toBeGreaterThan(0);
    }
  });

  it("keeps the advisory boundary visible here too", async () => {
    await renderAt(RUN_URL);
    await screen.findByTestId("scene");
    expect(screen.getByText(/ADVISORY ONLY/)).toBeTruthy();
  });

  it("points at the runs index when no run is named", async () => {
    // No `run` param: this is a legitimate state the screen names rather than
    // a fixture it falls back to, so the hook must never even be asked.
    await renderAt(`/cascade?valid=${VALID}`);
    expect(await screen.findByText(/no cascade run selected/i)).toBeTruthy();
    expect(screen.getByRole("link", { name: /runs/i })).toBeTruthy();
  });

  it("renders a run named in the URL", async () => {
    cascadeQuery.current = loadedRun(MINIMAL_RUN);
    await renderAt(RUN_URL);
    expect(await screen.findByText(/TULIA/i)).toBeTruthy();
  });

  it("surfaces a failed fetch rather than showing an empty cascade", async () => {
    cascadeQuery.current = failed;
    await renderAt(RUN_URL);
    // A cascade that failed to load must not be indistinguishable from a
    // cascade in which nothing happened — that is the whole reason this state
    // is rendered rather than falling through to an empty event log.
    expect(await screen.findByRole("alert")).toBeTruthy();
  });

  it("shows absent figures as not computed rather than as zero", async () => {
    // No wire run, ever, carries `customersInterrupted` — the engine has no
    // customer model — so rendering 0 would read as a finding, that no
    // customers were affected, rather than the truth: nobody computed it.
    // `findAllByText` rather than `findByText`: every one of the four absent
    // figures says "not computed", so more than one match is the correct,
    // expected outcome here, not a broken query.
    cascadeQuery.current = loadedRun(MINIMAL_RUN);
    await renderAt(RUN_URL);
    expect((await screen.findAllByText(/not computed/i)).length).toBeGreaterThan(0);
    expect(screen.queryByText(/^0 customers$/i)).toBeNull();
    expect(screen.queryByText(/^0$/)).toBeNull();
  });

  it("treats wire island 0 as a real island, not an absent one", async () => {
    // Wire island indices are 0-based; the domain's IslandId is 1-based
    // (min 1). Without the adapter's +1 shift, this run's only event names
    // island 0, which fails that bound, safeParse fails, and the screen
    // renders the malformed/alert state for what is actually an ordinary
    // cascade — every run whose first event touches the first-formed island.
    cascadeQuery.current = loadedRun(ISLAND_ZERO_RUN);
    await renderAt(RUN_URL);
    await screen.findByTestId("scene");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getAllByText("Island 1").length).toBeGreaterThan(0);
  });
});

describe("runs screen", () => {
  it("says the platform is unreachable rather than showing an empty table", async () => {
    runsQuery.current = failed;
    await renderAt("/runs");
    expect(screen.getByText(/platform is not reachable/i)).toBeTruthy();
    expect(screen.getByText(/Failed to fetch/)).toBeTruthy();
  });

  it("distinguishes an empty result from a failure", async () => {
    runsQuery.current = loaded([]);
    await renderAt("/runs");
    // An empty table and a broken connection look identical otherwise, and
    // only one of them means "nothing has been run yet".
    expect(screen.getByText(/No sweeps recorded/i)).toBeTruthy();
    expect(screen.queryByText(/not reachable/i)).toBeNull();
  });

  it("shows a spinner state while the request is in flight", async () => {
    runsQuery.current = loading;
    await renderAt("/runs");
    expect(screen.getByText(/Loading sweeps/i)).toBeTruthy();
  });

  it("lists the sweeps the platform returned", async () => {
    runsQuery.current = loaded([SWEEP]);
    await renderAt("/runs");
    expect(screen.getByText("1 runs")).toBeTruthy();
    expect(screen.getByText(SWEEP.id)).toBeTruthy();
    expect(screen.getByText("0.3.1")).toBeTruthy();
  });

  it("says plainly which columns the platform does not serve yet", async () => {
    runsQuery.current = loaded([SWEEP]);
    await renderAt("/runs");
    // The reviewed design promised expected unserved energy and a repro
    // verdict. Neither exists at this phase, and the screen should admit that
    // rather than render an empty column.
    expect(screen.getByText(/not served yet/i)).toBeTruthy();
  });
});

describe("runs screen, narrow", () => {
  beforeEach(() => {
    stubViewport(false);
    runsQuery.current = loaded([SWEEP]);
  });

  it("becomes a card list rather than a table to drag sideways", async () => {
    await renderAt("/runs");
    expect(screen.queryByRole("table")).toBeNull();
    expect(screen.getByText(/1 candidates/)).toBeTruthy();
  });
});
