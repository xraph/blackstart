import { describe, expect, it } from "vitest";
import {
  ScreeningCandidate,
  ScreeningList,
  evaluated,
  sortScreening,
  type Screenability,
} from "./screening";

function row(
  branch: number,
  screenability: Screenability,
  worstLoading?: number,
  violations = 0,
): unknown {
  return {
    contingencyId: `c-${branch}`,
    branch,
    screenability,
    ...(worstLoading === undefined ? {} : { worstLoading }),
    violations: Array.from({ length: violations }, (_, i) => ({
      branch: i,
      loading: 1.2,
      pFlow: 480,
      rating: 400,
    })),
  };
}

const branches = (rows: ScreeningList): number[] => rows.map((r) => r.branch);

describe("ScreeningCandidate", () => {
  it("keeps an absent worst loading absent", () => {
    // A radial outage does not have a loading of zero; it has no loading, and
    // that difference is the whole reason screenability exists.
    const parsed = ScreeningCandidate.parse(row(7, "radial"));

    expect(parsed.worstLoading).toBeUndefined();
    expect("worstLoading" in parsed).toBe(false);
  });

  it("carries a loading through when the screen produced one", () => {
    expect(ScreeningCandidate.parse(row(7, "screened", 1.34)).worstLoading).toBe(1.34);
  });

  it("rejects a screenability the platform does not issue", () => {
    expect(ScreeningCandidate.safeParse(row(7, "unknown" as Screenability)).success).toBe(false);
  });

  it("rejects a loading on an outage that could not be evaluated", () => {
    // The proto says worst loading is "absent when screenability is not
    // SCREENED". A row carrying both renders fine and ranks by a number
    // nothing measured, which is the failure worth catching at the boundary.
    expect(ScreeningCandidate.safeParse(row(7, "radial", 1.8)).success).toBe(false);
    expect(ScreeningCandidate.safeParse(row(7, "not-applicable", 0.2)).success).toBe(false);
  });

  it("allows a screened outage with no loading, which the solver never rules out", () => {
    // Enforcing the converse would reject payloads the platform is entitled
    // to send. The invariant runs one way and so does the check.
    expect(ScreeningCandidate.safeParse(row(7, "screened")).success).toBe(true);
  });

  it("treats only a screened outage as evaluated", () => {
    expect(evaluated("screened")).toBe(true);
    expect(evaluated("radial")).toBe(false);
    expect(evaluated("not-applicable")).toBe(false);
  });
});

describe("sortScreening", () => {
  const ROWS = ScreeningList.parse([
    row(10, "screened", 1.9, 3),
    row(20, "radial"),
    row(30, "screened", 2.4, 1),
    row(40, "not-applicable"),
    row(50, "screened", 0.8, 0),
  ]);

  it("keeps unevaluated candidates ahead of every evaluated one, by loading", () => {
    // The sweep's argument, in the sweep's own words: a radial outage that
    // islands part of the network has no loading to rank by, and ranking it
    // by the loading it does not have buries the most consequential case.
    expect(branches(sortScreening(ROWS, "loading"))).toEqual([20, 40, 30, 10, 50]);
  });

  it("keeps them ahead under every other key too", () => {
    // The reader chooses what orders the evaluated ones, not whether the
    // unknown ones stay visible.
    expect(branches(sortScreening(ROWS, "violations"))).toEqual([20, 40, 10, 30, 50]);
    expect(branches(sortScreening(ROWS, "branch"))).toEqual([20, 40, 10, 30, 50]);
  });

  it("breaks ties on the branch index, so one ranking reads the same way twice", () => {
    const tied = ScreeningList.parse([
      row(90, "screened", 1.5, 1),
      row(10, "screened", 1.5, 1),
      row(50, "screened", 1.5, 1),
    ]);

    expect(branches(sortScreening(tied, "loading"))).toEqual([10, 50, 90]);
  });

  it("does not mutate what it was given", () => {
    const before = branches(ROWS);
    sortScreening(ROWS, "loading");

    expect(branches(ROWS)).toEqual(before);
  });

  it("sorts a missing loading below a measured zero", () => {
    // Only reachable for a screened candidate the solver gave no loading for,
    // since the schema keeps the field off the unevaluated ones. "We have no
    // number" must not outrank a number that was measured and is 0.
    const rows = ScreeningList.parse([row(10, "screened"), row(20, "screened", 0)]);

    expect(branches(sortScreening(rows, "loading"))).toEqual([20, 10]);
  });
});
