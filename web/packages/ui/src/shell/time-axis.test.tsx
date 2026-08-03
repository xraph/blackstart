// @vitest-environment jsdom
/**
 * The two axes must stay two axes.
 *
 * Every simplification pressure on this component points at merging them, and
 * merging them is what makes a backtest indistinguishable from a live view.
 * These assertions exist so that change fails loudly.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ForecastHorizon, KnowledgeTime, ValidTime } from "@blackstart/schema";
import { TimeAxis } from "./time-axis";

beforeEach(() => {
  vi.stubGlobal("matchMedia", (q: string) => ({
    matches: false,
    media: q,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
});
afterEach(cleanup);

const HORIZON = ForecastHorizon.parse({
  cycle: "HRRR 2026-08-02 18Z",
  issued: "2026-08-02T18:52Z",
  leadHours: Array.from({ length: 19 }, (_, i) => i),
});

const LIVE = {
  validTime: ValidTime.parse("2026-08-02T22:00Z"),
  knowledgeTime: KnowledgeTime.parse("2026-08-02T18:14Z"),
};

const BACKTEST = {
  validTime: ValidTime.parse("2019-05-18T03:00Z"),
  knowledgeTime: KnowledgeTime.parse("2019-05-18T00:00Z"),
};

describe("TimeAxis", () => {
  it("scrubs with one labelled control rather than a row of hairline buttons", () => {
    const onScrub = vi.fn();
    render(
      <TimeAxis temporal={LIVE} horizon={HORIZON} leadHour={4} mode="live" onScrubValid={onScrub} />,
    );
    const slider = screen.getByRole("slider", { name: /forecast hour/i });
    expect(slider.getAttribute("min")).toBe("0");
    expect(slider.getAttribute("max")).toBe("18");

    fireEvent.change(slider, { target: { value: "9" } });
    expect(onScrub).toHaveBeenCalledWith(9);
  });

  it("shows the valid time", () => {
    render(<TimeAxis temporal={LIVE} horizon={HORIZON} leadHour={4} mode="live" />);
    expect(screen.getByText("2026-08-02 22:00Z")).toBeTruthy();
  });

  it("names the knowledge track separately, never merged into the valid one", () => {
    render(<TimeAxis temporal={LIVE} horizon={HORIZON} leadHour={4} mode="live" />);
    expect(screen.getByText("Valid time")).toBeTruthy();
    expect(screen.getByText("KNOWLEDGE TIME")).toBeTruthy();
  });

  it("says LIVE when knowledge time is current", () => {
    render(<TimeAxis temporal={LIVE} horizon={HORIZON} leadHour={4} mode="live" />);
    expect(screen.getByText(/NOW · LIVE/)).toBeTruthy();
  });

  it("names the as-of instant when pinned to the past", () => {
    render(<TimeAxis temporal={BACKTEST} horizon={HORIZON} leadHour={4} mode="as-of" />);
    expect(screen.getByText(/AS OF 2019-05-18 00:00Z/)).toBeTruthy();
  });

  it("carries the forecast cycle, so the axis says which run it is scrubbing", () => {
    render(<TimeAxis temporal={LIVE} horizon={HORIZON} leadHour={4} mode="live" />);
    expect(screen.getByText(/HRRR 2026-08-02 18Z/)).toBeTruthy();
  });
});
