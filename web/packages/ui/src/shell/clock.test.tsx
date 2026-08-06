// @vitest-environment jsdom
/**
 * The clock, which was a string literal.
 *
 * `clock="18:14:07 Z"` sat in the top bar of a forecast console. A stopped
 * clock is not a cosmetic gap — it asserts a time, and it is wrong within a
 * second of the page loading.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import { Clock } from "./clock";

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-08-05T18:14:07Z"));
});

afterEach(() => {
  vi.useRealTimers();
  cleanup();
});

describe("Clock", () => {
  it("prints the wall clock in UTC", () => {
    render(<Clock />);

    expect(screen.getByText("18:14:07 Z")).toBeTruthy();
  });

  it("advances", () => {
    // The whole point. A test that only asserted the first render would pass
    // against the literal this replaces.
    render(<Clock />);

    act(() => void vi.advanceTimersByTime(3000));

    expect(screen.getByText("18:14:10 Z")).toBeTruthy();
  });

  it("says which time it is showing", () => {
    // The console distinguishes valid time from knowledge time everywhere
    // else, so a bare wall clock is a third quantity and has to name itself.
    render(<Clock />);

    expect(screen.getByText("NOW")).toBeTruthy();
  });

  it("shows no as-of segment while the console is live", () => {
    render(<Clock />);

    expect(screen.queryByText(/as of/i)).toBeNull();
  });

  it("names the knowledge time it is pinned to", () => {
    render(<Clock knownAt="2026-08-05T14:00:00Z" />);

    expect(screen.getByText(/as of/i)).toBeTruthy();
    expect(screen.getByText("14:00 Z")).toBeTruthy();
  });

  it("goes on ticking while pinned", () => {
    // Two different quantities: the wall clock is still the wall clock during
    // a backtest. Freezing it would conflate the axes this console keeps
    // apart everywhere else.
    render(<Clock knownAt="2026-08-05T14:00:00Z" />);

    act(() => void vi.advanceTimersByTime(2000));

    expect(screen.getByText("18:14:09 Z")).toBeTruthy();
    expect(screen.getByText("14:00 Z")).toBeTruthy();
  });

  it("pads every field so the bar does not reflow each second", () => {
    vi.setSystemTime(new Date("2026-08-05T04:05:06Z"));
    render(<Clock />);

    expect(screen.getByText("04:05:06 Z")).toBeTruthy();
  });
});
