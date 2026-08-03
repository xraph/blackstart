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

import { routeTree } from "../router";
import { CASCADE, RUNS } from "../fixtures/derecho";

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
  render(<RouterProvider router={router} />);
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
  it("lists every run", async () => {
    await renderAt("/runs");
    expect(screen.getByText(`${RUNS.length} runs`)).toBeTruthy();
  });

  it("asks for a selection before offering a comparison", async () => {
    await renderAt("/runs");
    expect(screen.getByText(/Pick two runs to compare/)).toBeTruthy();
  });

  it("reports the delta when two runs share both time axes", async () => {
    const [a, b] = RUNS; // both derecho, same valid and knowledge time
    await renderAt(`/runs?compare=${a?.id}&compare=${b?.id}`);
    expect(screen.getByText(/Δ EUE/)).toBeTruthy();
    expect(screen.getByText(/same valid and knowledge time/)).toBeTruthy();
  });

  it("refuses to difference runs from different knowledge states, and says why", async () => {
    const live = RUNS[0];
    const backtest = RUNS.find((r) => r.scenario.startsWith("BACKTEST"));
    await renderAt(`/runs?compare=${live?.id}&compare=${backtest?.id}`);
    expect(screen.getByText(/mix a change in the scenario/)).toBeTruthy();
    expect(screen.queryByText(/Δ EUE/)).toBeNull();
  });
});

describe("runs screen, narrow", () => {
  beforeEach(() => stubViewport(false));

  it("becomes a card list rather than a table to drag sideways", async () => {
    await renderAt("/runs");
    expect(screen.queryByRole("table")).toBeNull();
    expect(screen.getAllByRole("button", { pressed: false }).length).toBeGreaterThan(0);
  });

  it("keeps both time axes on the card, so a backtest is still identifiable", async () => {
    await renderAt("/runs");
    expect(screen.getAllByText(/^valid /).length).toBe(RUNS.length);
    expect(screen.getAllByText(/^known /).length).toBe(RUNS.length);
  });
});
