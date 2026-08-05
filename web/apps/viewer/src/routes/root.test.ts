/**
 * The identity badge's two letters.
 *
 * Authsome records no display name for a password sign-up, so an address is
 * all the badge has to work with. What matters here is that it never invents
 * a letter: a manufactured second initial reads as a name the platform holds,
 * and it does not hold one.
 */

import { describe, expect, it } from "vitest";
import { initialsOf } from "./root";

describe("initialsOf", () => {
  it("takes two letters from a separated local part", () => {
    expect(initialsOf("rex.raphael@grid.test")).toBe("RR");
  });

  it("reads underscores and hyphens as the same separator", () => {
    expect(initialsOf("rex_raphael@grid.test")).toBe("RR");
    expect(initialsOf("rex-raphael@grid.test")).toBe("RR");
  });

  it("gives one letter rather than inventing a second", () => {
    expect(initialsOf("operator@grid.test")).toBe("O");
  });

  it("ignores a third name rather than printing three letters", () => {
    expect(initialsOf("rex.i.raphael@grid.test")).toBe("RR");
  });

  it("shows a dash when there is no address at all", () => {
    // The badge renders before a session exists in no path this ships, but an
    // empty box is indistinguishable from a broken one, so it says nothing
    // explicitly.
    expect(initialsOf("")).toBe("—");
  });
});
