// @vitest-environment jsdom
/**
 * The URL drives the panel.
 *
 * This is the behaviour the whole search-param design exists for, so it is
 * asserted rather than checked by eye: a link carrying `sel` must open the
 * inspector on that element, and one without it must show the screening rail
 * — or state which step of the case → run → ranking chain is missing.
 *
 * The globe is stubbed. WebGL does not exist in jsdom, and what is under test
 * is the routing and composition, not the renderer — which has its own tests
 * in @blackstart/globe.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { RouterProvider, createMemoryHistory, createRouter } from "@tanstack/react-router";

vi.mock("@blackstart/globe", async () => {
  const actual = await vi.importActual<typeof import("@blackstart/globe")>("@blackstart/globe");
  return { ...actual, Scene: () => <div data-testid="scene" /> };
});

import { ApiProvider } from "../api/provider";
import { AuthProvider } from "../auth/provider";
import { routeTree } from "../router";
import { LINE_TULIA_LEVELLAND } from "../fixtures/derecho";

/**
 * Viewport is stated per test rather than defaulted.
 *
 * jsdom reports no matches for anything, which silently puts every test in
 * the narrow layout — the opposite of what most of them mean to check.
 */
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

async function renderAt(url: string): Promise<void> {
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [url] }),
  });
  // The console fetches route geometry, so it needs the provider the app
  // supplies in main.tsx. The stubbed fetch below fails those requests, which
  // is the honest default for a test with no platform behind it.
  render(
    // The shell reads the signed-in operator for its identity badge, so the
    // auth provider is part of the frame every screen renders inside. Nothing
    // answers /me here, which settles it anonymous — the badge shows a dash
    // and the screens under test are unaffected.
    <AuthProvider publishableKey="pk_test">
      <ApiProvider>
        <RouterProvider router={router} />
      </ApiProvider>
    </AuthProvider>,
  );
  await screen.findByTestId("scene");
}

const VALID = "2026-08-02T22:00Z";

describe("console screen", () => {
  it("shows the screening rail when nothing is selected", async () => {
    await renderAt(`/console?valid=${VALID}`);
    expect(screen.getByText("N-1 screening")).toBeTruthy();
  });

  it("asks for a case rather than showing an empty rail", async () => {
    // With no case there is no content hash, so no run, so nothing to rank.
    // Saying which step is missing is what tells the reader where to look —
    // a spinner or a blank rail says the same thing for four causes.
    await renderAt(`/console?valid=${VALID}`);
    expect(screen.getByText(/Choose a case in the top bar/)).toBeTruthy();
  });

  it("offers the sort and the scope as controls that open", async () => {
    await renderAt(`/console?valid=${VALID}`);
    expect(screen.getByRole("combobox", { name: /sort the screening/i })).toBeTruthy();
    expect(screen.getByRole("combobox", { name: /how much of the screening/i })).toBeTruthy();
  });

  it("opens the inspector when the URL names an element", async () => {
    await renderAt(`/console?valid=${VALID}&sel=${LINE_TULIA_LEVELLAND}`);
    expect(screen.getByRole("heading", { name: /TULIA — LEVELLAND 345 kV/ })).toBeTruthy();
    // The rail is replaced, not covered — a modal would leave both mounted.
    expect(screen.queryByText("N-1 screening")).toBeNull();
  });

  it("says so plainly when the selected element has no explanation", async () => {
    await renderAt(`/console?valid=${VALID}&sel=ln_01k3qf8m2xw7rb4n09ytzzzzzz`);
    expect(screen.getByText(/has not been scored in this run/)).toBeTruthy();
  });

  it("keeps the advisory boundary visible on every screen", async () => {
    await renderAt(`/console?valid=${VALID}`);
    expect(screen.getByText(/ADVISORY ONLY/)).toBeTruthy();
  });

  it("reports a missing network export instead of rendering an empty globe", async () => {
    await renderAt(`/console?valid=${VALID}`);
    expect(await screen.findByText(/model\.json returned 404/)).toBeTruthy();
  });

  it("mounts the rail exactly once, not once per breakpoint", async () => {
    await renderAt(`/console?valid=${VALID}`);
    // Rendering both layouts and hiding one with CSS puts two copies of every
    // control in the document — twice the work, and two tab stops each.
    expect(screen.getAllByText("N-1 screening")).toHaveLength(1);
  });
});

describe("console screen, narrow", () => {
  beforeEach(() => stubViewport(false));

  it("reflows the rail into a collapsed sheet rather than docking it", async () => {
    await renderAt(`/console?valid=${VALID}`);
    const handle = screen.getByRole("button", { name: /n-1 screening/i });
    expect(handle.getAttribute("aria-expanded")).toBe("false");
    // Collapsed means unmounted, so the body is not a silent tab stop.
    expect(screen.queryByText(/Choose a case in the top bar/)).toBeNull();
  });

  it("gives the inspector the whole stage, since 480px does not fit beside a globe", async () => {
    await renderAt(`/console?valid=${VALID}&sel=${LINE_TULIA_LEVELLAND}`);
    expect(screen.getByRole("heading", { name: /TULIA — LEVELLAND 345 kV/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /n-1 screening/i })).toBeNull();
  });
});
