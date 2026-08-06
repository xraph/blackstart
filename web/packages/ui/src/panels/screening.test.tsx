// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ScreeningList } from "@blackstart/schema";
import { ScreeningRail } from "./screening";

afterEach(cleanup);

const ROWS = ScreeningList.parse([
  { contingencyId: "c-20", branch: 20, screenability: "radial", violations: [] },
  {
    contingencyId: "c-30",
    branch: 30,
    screenability: "screened",
    worstLoading: 1.34,
    violations: [{ branch: 7, loading: 1.34, pFlow: 536, rating: 400 }],
  },
  {
    contingencyId: "c-50",
    branch: 50,
    screenability: "screened",
    worstLoading: 0.82,
    violations: [],
  },
]);

describe("ScreeningRail", () => {
  it("prints a missing loading as a dash, never as a number", () => {
    // A zero here would rank as the safest thing on the list, which is the
    // inversion the whole screenability field exists to prevent.
    render(<ScreeningRail rows={ROWS} />);

    expect(screen.getByTitle("not evaluated").textContent).toBe("—");
    expect(screen.queryByText("0%")).toBeNull();
  });

  it("says why an outage could not be evaluated rather than leaving it blank", () => {
    render(<ScreeningRail rows={ROWS} />);

    expect(screen.getByText(/islands the network/)).toBeTruthy();
  });

  it("names branches by index, which is all the model records", () => {
    render(<ScreeningRail rows={ROWS} />);

    expect(screen.getByText("branch 30")).toBeTruthy();
  });

  it("renders the order it is given rather than sorting again", () => {
    // The sort key lives in the URL and sortScreening applies it. A rail that
    // re-sorted would silently ignore the control above it.
    render(<ScreeningRail rows={ROWS} />);

    const rendered = screen.getAllByRole("button").map((b) => b.textContent ?? "");
    expect(rendered[0]).toContain("branch 20");
    expect(rendered[2]).toContain("branch 50");
  });

  it("reports the candidate that was chosen", () => {
    const onSelect = vi.fn();
    render(<ScreeningRail rows={ROWS} onSelect={onSelect} />);

    fireEvent.click(screen.getByRole("button", { name: /branch 30/ }));

    expect(onSelect).toHaveBeenCalledOnce();
    expect(onSelect.mock.calls[0]?.[0]?.branch).toBe(30);
  });

  it("distinguishes a clean screen from an empty filter", () => {
    // "Nothing concerning" and "nothing at all" look identical as a blank
    // panel, and only one of them is answered by widening the filter.
    render(<ScreeningRail rows={ScreeningList.parse([])} />);

    expect(screen.getByText(/warrants verification/)).toBeTruthy();
  });
});
