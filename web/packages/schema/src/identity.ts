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
 * Every prefix the client can encounter, with the noun an operator would use.
 * The description is the label the interface shows, so adding a prefix here is
 * what makes it displayable.
 */
export const TYPE_PREFIXES = {
  ln: "transmission line",
  bus: "bus",
  sub: "substation",
  str: "structure",
  span: "span",
  gen: "generator",
  scn: "scenario",
  run: "run",
  casc: "cascade run",
  crun: "contingency run",
  nmdl: "network model",
  mdl: "model",
  ds: "dataset",
  fs: "feature set",
  tx: "network version",
} as const;

/*
 * This table has drifted from what the platform issues, and `crun` and `nmdl`
 * are the two the console now needs — a network model comes back as
 * `nmdl_…` and was being rejected as not a TypeID at all.
 *
 * The drift runs both ways and is wider than these two: `extensions/*` also
 * issue `srun`, `rte`, `strc`, `dsrc`, `sub`, `vlev` and more, while `mdl`,
 * `ds`, `fs` and `tx` below are issued by nothing. Reconciling the whole
 * table is worth doing and is not this change — adding only what is used
 * keeps the list honest about being incomplete rather than looking settled.
 */

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

/** The prefix of an identifier, or null if it is not a TypeID at all. */
export function prefixOf(id: string): TypePrefix | null {
  const under = id.indexOf("_");
  if (under < 1) return null;
  const p = id.slice(0, under);
  return p in TYPE_PREFIXES ? (p as TypePrefix) : null;
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
