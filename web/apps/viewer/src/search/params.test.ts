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

  it("defaults the drawn layer to the rating, which is the phase's headline", () => {
    expect(consoleSearch.parse({ valid: "2026-08-02T22:00Z" }).layer).toBe("ampacity");
  });

  it("carries which layer is drawn, because a screenshot has to be reproducible", () => {
    const s = consoleSearch.parse({ valid: "2026-08-02T22:00Z", layer: "loading" });
    expect(s.layer).toBe("loading");
  });

  it("refuses a layer the console cannot draw rather than falling back to one it can", () => {
    // A URL naming the clouds layer is asking for something this build does
    // not have. Quietly showing ampacity instead answers a question nobody
    // asked, under a link that says otherwise.
    expect(consoleSearch.safeParse({ valid: "2026-08-02T22:00Z", layer: "clouds" }).success).toBe(
      false,
    );
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
    expect(consoleSearch.safeParse({ valid: "2026-08-02T22:00Z", sel: "LINE-4471" }).success).toBe(
      false,
    );
  });

  it("rejects a lead hour outside the published horizon", () => {
    expect(consoleSearch.safeParse({ valid: "2026-08-02T22:00Z", lead: 99 }).success).toBe(false);
  });

  it("coerces the lead hour, since a search param arrives as a string", () => {
    expect(consoleSearch.parse({ valid: "2026-08-02T22:00Z", lead: "6" }).lead).toBe(6);
  });

  it("leaves the scenario unset rather than naming one nobody chose", () => {
    // It used to default to the slug `tx-2026-0802-derecho`, which the
    // platform has never issued: a Scenario is `{id, name, manifest}` with no
    // slug at all, so that default named nothing.
    const s = consoleSearch.parse({ valid: "2026-08-02T22:00Z" });

    expect(s.scenario).toBeUndefined();
    expect(s.case).toBeUndefined();
  });

  it("carries the scenario and the case as the identifiers the platform issues", () => {
    const s = consoleSearch.parse({
      valid: "2026-08-02T22:00Z",
      scenario: "scn_01k3qf8m2xw7rb4n09ytvdhjkm",
      case: "nmdl_01k3qf8m2xw7rb4n09ytvdhjkn",
    });

    expect(s.scenario).toBe("scn_01k3qf8m2xw7rb4n09ytvdhjkm");
    expect(s.case).toBe("nmdl_01k3qf8m2xw7rb4n09ytvdhjkn");
  });

  it("rejects the old slug rather than resolving it against a name", () => {
    // Names are neither unique nor stable, so matching one would let a rename
    // silently break a cited URL.
    expect(
      consoleSearch.safeParse({ valid: "2026-08-02T22:00Z", scenario: "tx-2026-0802-derecho" })
        .success,
    ).toBe(false);
  });

  it("refuses a scenario identifier in the case slot", () => {
    // The two are branded apart in the schema for this reason: the case
    // determines which run the rail ranks, and the wrong one produces an
    // empty rail rather than an error.
    expect(
      consoleSearch.safeParse({
        valid: "2026-08-02T22:00Z",
        case: "scn_01k3qf8m2xw7rb4n09ytvdhjkm",
      }).success,
    ).toBe(false);
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

  it("admits the scenario kind, since a scenario run belongs in the same index", () => {
    expect(runsSearch.parse({ kind: "scenario" }).kind).toBe("scenario");
  });

  it("rejects a kind the index has never heard of, rather than falling back to unfiltered", () => {
    // Distinct from the acceptance test above: a schema that defaults on any
    // unrecognized string would pass "scenario" for the wrong reason.
    expect(runsSearch.safeParse({ kind: "nonsense" }).success).toBe(false);
  });
});
