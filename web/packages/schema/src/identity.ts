/**
 * Type-prefixed identifiers.
 *
 * The platform issues TypeIDs — a type prefix, an underscore, and a 26
 * character Crockford base32 suffix. Encoding the prefix in the *type* means a
 * component that takes a line identifier cannot be handed a run identifier,
 * which is the mistake that produces an empty panel and no error.
 *
 * The alias table matters as much as the identifier. Reconciliation across
 * ACTIVSg, HIFLD, OSM and CIM must stay auditable and reversible, so a source
 * identifier is never discarded once it has resolved to a TypeID — it travels
 * alongside as an {@link Alias}.
 */

import { z } from "zod";

/**
 * Prefixes this client accepts that no extension issues.
 *
 * Separate from the rest of the table rather than commented, because the drift
 * test uses this object as its allowlist: a prefix the platform does not mint
 * has to be declared here deliberately, and cannot be added to the main table
 * by someone who assumed it was real.
 *
 * Every entry is a claim waiting to be settled, not a stable category:
 *
 * - `ln` is consumed — `TransmissionLine.id` and the ranked contingency's
 *   `element` are both typed on it — but the platform issues no `ln`. The two
 *   real spellings of that concept are `br`, a network model's branch, and
 *   `circ`, a line somebody actually built; `extensions/geo/id` is explicit
 *   that those are different things. Whichever one the API returns, this
 *   should become, and the schemas that name `ln` move with it.
 * - `bus`, `gen` and `str` are consumed by nothing and issued by nothing.
 *   They survive the reconciliation only because removing them was not asked
 *   for; on the evidence they are as dead as `mdl`, `ds`, `fs` and `tx`, which
 *   this change removed. `str` is the most suspect of the three — geo issues
 *   `strc` for a structure, so `str` reads as a stale spelling of it rather
 *   than a separate entity.
 */
export const CLIENT_ONLY_PREFIXES = {
  ln: "transmission line",
  bus: "bus",
  gen: "generator",
  str: "structure",
} as const;

/**
 * Every prefix the client can encounter, with the noun an operator would use.
 * The description is the label the interface shows, so adding a prefix here is
 * what makes it displayable.
 *
 * Grouped by the extension that owns the prefix, because that is the boundary
 * the platform enforces: `pkg/id` refuses to let two extensions claim the same
 * prefix, and a table grouped any other way makes it hard to see which owner a
 * new entry would collide with.
 *
 * This table is the gate on `AnyTypeId`, which is what the console's `sel`
 * search param parses. A prefix missing here is not a cosmetic omission — the
 * element cannot be selected or linked to at all, and the failure reads as a
 * broken inspector rather than as an identifier nobody taught the client
 * about. `identity.drift.test.ts` asserts this list against the Go source so
 * that a new extension prefix fails the suite rather than a panel.
 */
export const TYPE_PREFIXES = {
  /* cascade */
  casc: "cascade run",

  /* contingency */
  cset: "contingency set",
  crun: "contingency run",

  /* datasource */
  dsrc: "data source",
  dfeed: "feed",

  /* geo */
  rte: "route",
  span: "span",
  strc: "structure",
  geom: "footprint",
  circ: "circuit",

  /* hazard */
  hzcnd: "conductor",
  hzasg: "conductor assignment",
  hzcmp: "hazard computation",

  /* networkmodel */
  nmdl: "network model",
  nmblob: "network model encoding",
  br: "branch",
  nmplc: "bus placement",
  nmbus: "bus",

  /* powerflow */
  pfsol: "power flow solution",

  /* provenance */
  dsv: "dataset version",
  mver: "model version",
  run: "run",
  lnge: "lineage",

  /* scenario */
  scn: "scenario",
  srun: "scenario run",

  /* scl */
  ied: "intelligent electronic device",
  gpub: "GOOSE publication",
  gsub: "GOOSE subscription",

  /* topology */
  sub: "substation",
  vlev: "voltage level",
  bay: "bay",
  cnode: "connectivity node",
  term: "terminal",
  equip: "equipment",
  alias: "external alias",

  /* weather */
  wxcyc: "forecast cycle",
  wxgrd: "forecast grid",
  wxstn: "weather station",

  ...CLIENT_ONLY_PREFIXES,
} as const;

export type TypePrefix = keyof typeof TYPE_PREFIXES;

/** Crockford base32: digits plus lowercase letters, less i, l, o and u. */
const SUFFIX = "[0-9abcdefghjkmnpqrstvwxyz]{26}";

/**
 * A TypeID schema for one prefix.
 *
 * Branded on the prefix, so `TypeId<"ln">` and `TypeId<"run">` are distinct
 * types even though both are strings at runtime.
 */
export function typeId<P extends TypePrefix>(prefix: P) {
  return z
    .string()
    .regex(
      new RegExp(`^${prefix}_${SUFFIX}$`, "i"),
      `expected a ${TYPE_PREFIXES[prefix]} identifier of the form ${prefix}_…`,
    )
    .brand<`typeid:${P}`>();
}

export type TypeId<P extends TypePrefix> = z.infer<ReturnType<typeof typeId<P>>>;

/** Any TypeID, for the few places that genuinely accept a heterogeneous id. */
export const AnyTypeId = z
  .string()
  .regex(new RegExp(`^(${Object.keys(TYPE_PREFIXES).join("|")})_${SUFFIX}$`, "i"));

/**
 * The prefix of an identifier, or null if it is not a TypeID at all.
 *
 * `Object.hasOwn` rather than `in`, which walks the prototype chain: `in`
 * answers true for every key on `Object.prototype`, so `constructor_…`,
 * `toString_…` and `valueOf_…` all reported a prefix this table never declared.
 * `AnyTypeId` rejects those strings — its regex is built from the table's own
 * keys — so the two disagreed about the same identifier, and only the looser
 * one was used to decide what a value is rather than whether it is valid.
 */
export function prefixOf(id: string): TypePrefix | null {
  const under = id.indexOf("_");
  if (under < 1) return null;
  const p = id.slice(0, under);
  return Object.hasOwn(TYPE_PREFIXES, p) ? (p as TypePrefix) : null;
}

/**
 * A TypeID shortened for display, keeping the prefix and the tail.
 *
 * The tail rather than the head because the suffix is ULID-like: the leading
 * characters are a timestamp and are near-identical across records created in
 * the same session, so truncating from the right would make every identifier
 * on screen look the same.
 */
export function abbreviateTypeId(id: string, tail = 6): string {
  const under = id.indexOf("_");
  if (under < 1 || id.length - under - 1 <= tail) return id;
  return `${id.slice(0, under + 1)}…${id.slice(-tail)}`;
}

/**
 * An external identifier that resolved to a TypeID.
 *
 * `system` is the source of record and `authority` says what that source is
 * trusted for — OpenStreetMap is authoritative for structure geometry and
 * carries no impedances or ratings at all, ACTIVSg is exactly the reverse, and
 * an operator reading a conflict needs to know which.
 */
export const Alias = z.object({
  system: z.string().min(1),
  externalId: z.string().min(1),
  authority: z.enum(["topology", "geometry", "route", "ratings", "naming"]),
});
export type Alias = z.infer<typeof Alias>;
