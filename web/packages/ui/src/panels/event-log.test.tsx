// @vitest-environment jsdom
/**
 * The event log's order.
 *
 * The rail's control read `REVERSE ▾` and did nothing. It also never said
 * reverse of *what* — the log has always called `newestFirst` unconditionally,
 * so the reader had no stated order to reverse from.
 */

import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import type { CascadeEvent } from "@blackstart/schema";
import { EventLog } from "./event-log";

afterEach(cleanup);

const EVENTS: CascadeEvent[] = [
  { t: 0, kind: "trip", element: "ln FIRST", detail: "initiating" },
  { t: 1.18, kind: "overload", element: "ln SECOND", detail: "108%" },
  { t: 2.94, kind: "trip", element: "ln THIRD", detail: "overload" },
];

const order = (): string[] =>
  screen.getAllByRole("listitem").map((li) => li.textContent?.match(/ln [A-Z]+/)?.[0] ?? "");

describe("EventLog", () => {
  it("reads newest first by default, which is how a log reads", () => {
    render(<EventLog events={EVENTS} />);

    expect(order()).toEqual(["ln THIRD", "ln SECOND", "ln FIRST"]);
  });

  it("reads oldest first when asked, which is the order the cascade happened in", () => {
    render(<EventLog events={EVENTS} order="oldest" />);

    expect(order()).toEqual(["ln FIRST", "ln SECOND", "ln THIRD"]);
  });

  it("tints the most recent rows in both orders, not the first rows on screen", () => {
    // The tint means "just arrived". Keyed on screen position it would mark
    // the *oldest* events in oldest-first order — the opposite claim, made
    // silently.
    const { rerender } = render(<EventLog events={EVENTS} highlightCount={1} />);
    expect(screen.getAllByRole("listitem")[0]?.className).toContain("bg-hl");

    rerender(<EventLog events={EVENTS} order="oldest" highlightCount={1} />);
    const rows = screen.getAllByRole("listitem");
    expect(rows[2]?.className).toContain("bg-hl");
    expect(rows[0]?.className).not.toContain("bg-hl");
  });

  it("does not mutate the events it was handed", () => {
    const given = [...EVENTS];
    render(<EventLog events={given} order="oldest" />);

    expect(given.map((e) => e.element)).toEqual(EVENTS.map((e) => e.element));
  });
});
