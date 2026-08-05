/**
 * The identity badge's two letters.
 *
 * Registration records a name, so the badge prefers it. Where there is none —
 * an older account, or one created by other means — an address is all it has,
 * and what matters there is that it never invents a letter: a manufactured
 * second initial reads as a name the platform holds, and it does not hold one.
 */

import { describe, expect, it } from "vitest";
import { initialsOf, initialsOfSession } from "./root";

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

describe("initialsOfSession", () => {
  const session = (firstName: string, lastName: string, email = "operator@grid.test") => ({
    firstName,
    lastName,
    email,
  });

  it("prefers the recorded name over the address", () => {
    // The address would give "RI" here. A name somebody typed beats initials
    // guessed from the shape of their email.
    expect(initialsOfSession(session("Rex", "Raphael", "r.i.raphael@grid.test"))).toBe("RR");
  });

  it("takes one letter when only half a name was recorded", () => {
    expect(initialsOfSession(session("Rex", ""))).toBe("R");
    expect(initialsOfSession(session("", "Raphael"))).toBe("R");
  });

  it("falls back to the address when no name was recorded", () => {
    expect(initialsOfSession(session("", "", "rex.raphael@grid.test"))).toBe("RR");
  });

  it("treats whitespace as no name rather than as initials", () => {
    expect(initialsOfSession(session("  ", " ", "rex.raphael@grid.test"))).toBe("RR");
  });

  it("has nothing to show before anyone signs in", () => {
    expect(initialsOfSession(null)).toBe("—");
  });
});
