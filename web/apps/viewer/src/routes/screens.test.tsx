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
 * The runs screen fetches. Stubbing the generated hook keeps these tests about
 * the screen's states — loading, error, empty, populated — rather than about
 * the transport, which has its own tests in the client package.
 */
const runsQuery = vi.hoisted(() => ({
  current: { data: undefined, isPending: true, isError: false, error: null } as unknown,
}));

vi.mock("@blackstart/api-client", async () => {
  const actual =
    await vi.importActual<typeof import("@blackstart/api-client")>("@blackstart/api-client");
  return { ...actual, useContingencyRunsList: () => runsQuery.current };
});

import { ApiProvider } from "../api/provider";
import { routeTree } from "../router";
import { CASCADE } from "../fixtures/derecho";

const SWEEP = {
  id: "run_01k3qf8m2xw7rb4n09yvtvdhjk",
  modelHash: "9f2c1a7e44b0d3castille",
  solverVersion: "0.3.1",
  startedAt: "2026-08-02T22:00:00Z",
  finishedAt: "2026-08-02T22:04:18Z",
  candidates: [
    { branch: 4471, contingencyId: "c-1", screenability: "screened", violations: [
      { branch: 4471, loading: 1.18, pFlow: 412, rating: 350 },
    ] },
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

describe("cascade screen", () => {
  it("starts at the end of the run, showing every event", async () => {
    await renderAt(`/cascade?valid=${VALID}`);
    await screen.findByTestId("scene");
    expect(screen.getByText(`${CASCADE.events.length} / ${CASCADE.events.length}`)).toBeTruthy();
  });

  it("shows the initiating outage, which is the row the reader looks for first", async () => {
    await renderAt(`/cascade?valid=${VALID}`);
    await screen.findByTestId("scene");
    expect(screen.getByText("INITIATING")).toBeTruthy();
  });

  it("labels every island the run formed", async () => {
    await renderAt(`/cascade?valid=${VALID}`);
    await screen.findByTestId("scene");
    for (const island of CASCADE.islands) {
      expect(screen.getAllByText(`Island ${island.id}`).length).toBeGreaterThan(0);
    }
  });

  it("keeps the advisory boundary visible here too", async () => {
    await renderAt(`/cascade?valid=${VALID}`);
    await screen.findByTestId("scene");
    expect(screen.getByText(/ADVISORY ONLY/)).toBeTruthy();
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
