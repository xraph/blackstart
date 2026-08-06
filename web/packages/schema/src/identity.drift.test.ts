/**
 * The prefix table, checked against the platform that issues the prefixes.
 *
 * `TYPE_PREFIXES` gates `AnyTypeId`, which is what the console's `sel` search
 * param parses. An element whose prefix is absent from the table cannot be
 * selected or linked to at all, and the symptom is a panel that renders empty
 * rather than an error naming an unknown identifier — so the drift is both
 * silent and hard to read backwards from.
 *
 * It had drifted in both directions before this test existed: eleven
 * extensions were issuing prefixes the client rejected, while four entries in
 * the table named nothing the platform had ever minted. Both halves are
 * asserted here, because only checking coverage would let the table
 * accumulate fictions indefinitely.
 *
 * The Go source is read rather than a generated manifest because the prefixes
 * are declared as `const` in each extension's `id` package and registered from
 * `init`, so the only artefact that exists at rest is the source itself. That
 * is a coupling worth its cost: `pkg/id` says a shipped prefix is immutable,
 * which makes the declaration site as stable a thing to read as any file the
 * build could emit.
 */

import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { AnyTypeId, CLIENT_ONLY_PREFIXES, TYPE_PREFIXES } from "./identity";

/** `web/packages/schema/src` → the repository root. */
const REPO_ROOT = fileURLToPath(new URL("../../../..", import.meta.url));
const EXTENSIONS = join(REPO_ROOT, "extensions");

/**
 * A prefix declaration, in either shape the extensions use: a lone `const Run
 * id.Prefix = "casc"` or a member of a `const (…)` block. Comment lines are
 * dropped first so that prose naming a prefix cannot register as one.
 */
const DECLARATION = /\bid\.Prefix\s*=\s*"([^"]+)"/g;

/** Every prefix declared in an extension's `id/id.go`, by owning extension. */
function declaredPrefixes(): Map<string, string> {
  const owners = new Map<string, string>();

  for (const owner of readdirSync(EXTENSIONS, { withFileTypes: true })) {
    if (!owner.isDirectory()) continue;

    let source: string;
    try {
      source = readFileSync(join(EXTENSIONS, owner.name, "id", "id.go"), "utf8");
    } catch {
      // Not every extension declares identifiers of its own.
      continue;
    }

    const code = source
      .split("\n")
      .filter((line) => !line.trimStart().startsWith("//"))
      .join("\n");

    for (const [, prefix] of code.matchAll(DECLARATION)) {
      if (prefix) owners.set(prefix, owner.name);
    }
  }

  return owners;
}

const declared = declaredPrefixes();

describe("the prefix table against the extensions that issue prefixes", () => {
  /*
   * The scan is the test's foundation, so it is asserted before anything is
   * concluded from it. A wrong REPO_ROOT, a moved `extensions/` tree or a
   * regex that stopped matching would otherwise produce an empty set, and
   * every coverage assertion below would pass vacuously — the exact failure a
   * drift test exists to rule out.
   */
  it("reads the Go source it is checking against", () => {
    expect(declared.size).toBeGreaterThan(30);
    expect(new Set(declared.values()).size).toBeGreaterThan(8);

    // Spot-checked against their declaration sites, so that a regex which
    // silently matched a different construct fails here rather than passing
    // with a plausible-looking count.
    expect(declared.get("casc")).toBe("cascade");
    expect(declared.get("srun")).toBe("scenario");
    expect(declared.get("wxcyc")).toBe("weather");
  });

  it("covers every prefix an extension issues", () => {
    const missing = [...declared]
      .filter(([prefix]) => !(prefix in TYPE_PREFIXES))
      .map(([prefix, owner]) => `${prefix} (${owner})`);

    expect(missing, "prefixes the platform issues that the client would reject").toEqual([]);
  });

  /*
   * The other direction. A prefix in the table that nothing issues is not
   * inert: it widens `AnyTypeId`, so a malformed identifier can parse as a
   * selectable element, and it offers a `typeId("…")` that no response will
   * ever satisfy. Anything genuinely client-side belongs in
   * CLIENT_ONLY_PREFIXES, where it carries the reason it is there.
   */
  it("claims no prefix the platform does not issue, outside the declared exceptions", () => {
    const unissued = Object.keys(TYPE_PREFIXES).filter((prefix) => !declared.has(prefix));

    expect(unissued.sort()).toEqual(Object.keys(CLIENT_ONLY_PREFIXES).sort());
  });

  it("gives every prefix a label the interface can show", () => {
    for (const [prefix, description] of Object.entries(TYPE_PREFIXES)) {
      expect(description, `${prefix} has no description`).toMatch(/\S/);
    }
  });

  /*
   * `AnyTypeId` joins the keys into one alternation, which is where a prefix
   * that is itself the prefix of another — `str` and `strc`, `ds…` and `dsv` —
   * could shadow its neighbour if the pattern were ever anchored differently.
   * Asserting each prefix parses under the shared regex covers that.
   */
  it("accepts an identifier under every prefix in the table", () => {
    const suffix = "01k3qf8m2xw7rb4n09ytvdhjkm";

    for (const prefix of Object.keys(TYPE_PREFIXES)) {
      expect(AnyTypeId.safeParse(`${prefix}_${suffix}`).success, prefix).toBe(true);
    }
  });
});
