import { describe, expect, it } from "vitest";
import { consoleSearch, runsSearch, temporalOf, DEFAULT_CONSOLE_SEARCH } from "./params";

describe("console search params", () => {
  it("defaults knowledge time to absent, meaning live", () => {
    const s = consoleSearch.parse({ valid: "2026-08-02T22:00Z" });
    expect(s.known).toBeUndefined();
    expect(s.lead).toBe(DEFAULT_CONSOLE_SEARCH.lead);
  });

  it("carries an explicit knowledge time, which is what makes a URL a backtest", () => {
    const s = consoleSearch.parse({
      valid: "2019-05-18T03:00Z",
      known: "2019-05-18T00:00Z",
    });
    expect(temporalOf(s).knowledgeTime).toBe("2019-05-18T00:00Z");
  });

  it("rejects a non-UTC instant rather than silently reinterpreting it", () => {
    expect(consoleSearch.safeParse({ valid: "2026-08-02T22:00+02:00" }).success).toBe(false);
  });

  it("falls back to the valid time when no knowledge time is given", () => {
    const s = consoleSearch.parse({ valid: "2026-08-02T22:00Z" });
    expect(temporalOf(s).knowledgeTime).toBe("2026-08-02T22:00Z");
  });

  it("round-trips a selected element identifier", () => {
    const s = consoleSearch.parse({
      valid: "2026-08-02T22:00Z",
      sel: "ln_01k3qf8m2xw7rb4n09ytvdhjkm",
    });
    expect(s.sel).toBe("ln_01k3qf8m2xw7rb4n09ytvdhjkm");
  });

  it("rejects a selection that is not a TypeID, so a hand-edited URL fails loudly", () => {
    expect(
      consoleSearch.safeParse({ valid: "2026-08-02T22:00Z", sel: "LINE-4471" }).success,
    ).toBe(false);
  });

  it("rejects a lead hour outside the published horizon", () => {
    expect(consoleSearch.safeParse({ valid: "2026-08-02T22:00Z", lead: 99 }).success).toBe(false);
  });

  it("coerces the lead hour, since a search param arrives as a string", () => {
    expect(consoleSearch.parse({ valid: "2026-08-02T22:00Z", lead: "6" }).lead).toBe(6);
  });
});

describe("runs search params", () => {
  it("defaults to an unfiltered view with nothing selected", () => {
    const s = runsSearch.parse({});
    expect(s).toEqual({ kind: "all", status: "any", compare: [] });
  });

  it("holds at most two runs, because the exit from this screen is a diff", () => {
    expect(runsSearch.safeParse({ compare: ["a", "b", "c"] }).success).toBe(false);
  });
});
