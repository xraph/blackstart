/**
 * Typed electrical quantities.
 *
 * The Go side models every electrical value as magnitude plus unit plus a
 * per-unit-versus-SI reference base, precisely because mixing per-unit and
 * physical units is the classic power-systems bug class. The client inherits
 * that discipline rather than flattening back to bare numbers at the API
 * boundary — a `Quantity<"MW">` and a `Quantity<"kV">` are different types, so
 * handing megawatts to something expecting kilovolts is a compile error and
 * not a wrong number drawn on a globe.
 *
 * The second reason this exists: the interface rule is that no number renders
 * without its unit. A component that accepts `Quantity` instead of `number`
 * cannot forget, because the unit arrives attached to the value.
 */

import { z } from "zod";

/**
 * Every unit the client knows how to display.
 *
 * `toSi` is the multiplier onto the SI base unit and `offset` the additive
 * term applied after it, which only temperature needs. `decimals` is the
 * display precision an operator expects for that dimension — frequency is read
 * to two places because 59.62 Hz and 59.6 Hz are different operational
 * situations, whereas nobody reads megawatts to a fraction.
 */
export const UNITS = {
  MW: { dimension: "power", si: "W", toSi: 1e6, offset: 0, decimals: 0 },
  "MW·h": { dimension: "energy", si: "J", toSi: 3.6e9, offset: 0, decimals: 0 },
  kV: { dimension: "voltage", si: "V", toSi: 1e3, offset: 0, decimals: 0 },
  A: { dimension: "current", si: "A", toSi: 1, offset: 0, decimals: 0 },
  km: { dimension: "length", si: "m", toSi: 1e3, offset: 0, decimals: 1 },
  m: { dimension: "length", si: "m", toSi: 1, offset: 0, decimals: 1 },
  "km²": { dimension: "area", si: "m²", toSi: 1e6, offset: 0, decimals: 0 },
  "m·s⁻¹": { dimension: "speed", si: "m·s⁻¹", toSi: 1, offset: 0, decimals: 1 },
  "°C": { dimension: "temperature", si: "K", toSi: 1, offset: 273.15, decimals: 0 },
  Hz: { dimension: "frequency", si: "Hz", toSi: 1, offset: 0, decimals: 2 },
  "%": { dimension: "ratio", si: "1", toSi: 0.01, offset: 0, decimals: 0 },
  pu: { dimension: "ratio", si: "1", toSi: 1, offset: 0, decimals: 3 },
  customers: { dimension: "count", si: "1", toSi: 1, offset: 0, decimals: 0 },
} as const;

export type Unit = keyof typeof UNITS;
export type Dimension = (typeof UNITS)[Unit]["dimension"];

/** The units belonging to one dimension, so an API can accept `km` or `m`. */
export type UnitOf<D extends Dimension> = {
  [U in Unit]: (typeof UNITS)[U]["dimension"] extends D ? U : never;
}[Unit];

/**
 * A magnitude carrying its unit, and optionally the base it is per-unit of.
 *
 * `base` is only meaningful for `pu`. A per-unit value without its base is not
 * a quantity, it is a ratio nobody can convert, so the formatter refuses to
 * render one — see {@link formatQuantity}.
 */
export interface Quantity<U extends Unit = Unit> {
  readonly value: number;
  readonly unit: U;
  readonly base?: { readonly value: number; readonly unit: Unit } | undefined;
}

/**
 * Builds the Zod schema for one specific unit.
 *
 * Narrowing to a literal is the whole point: `quantity("MW")` will not accept
 * `{ value: 345, unit: "kV" }`, so a mis-typed API response fails at the
 * boundary rather than three components deep.
 */
export function quantity<U extends Unit>(unit: U) {
  return z.object({
    value: z.number().finite(),
    unit: z.literal(unit),
    base: z.object({ value: z.number().finite().positive(), unit: z.enum(unitNames()) }).optional(),
  });
}

/** The unit keys as a tuple, which `z.enum` needs and `Object.keys` loses. */
function unitNames(): [Unit, ...Unit[]] {
  const keys = Object.keys(UNITS) as Unit[];
  const [first, ...rest] = keys;
  if (first === undefined) throw new Error("UNITS is empty");
  return [first, ...rest];
}

/** Any quantity, when a component genuinely does not care which dimension. */
export const AnyQuantity = z.object({
  value: z.number().finite(),
  unit: z.enum(unitNames()),
  base: z.object({ value: z.number().finite().positive(), unit: z.enum(unitNames()) }).optional(),
});

/* ── constructors ────────────────────────────────────────────────────────
 * Short names because these appear inline in fixtures and tests constantly,
 * and `mw(4187)` reads better at a call site than a nine-character helper. */

const make =
  <U extends Unit>(unit: U) =>
  (value: number, base?: Quantity): Quantity<U> =>
    base === undefined
      ? { value, unit }
      : { value, unit, base: { value: base.value, unit: base.unit } };

export const mw = make("MW");
export const mwh = make("MW·h");
export const kv = make("kV");
export const amps = make("A");
export const km = make("km");
export const metres = make("m");
export const km2 = make("km²");
export const mps = make("m·s⁻¹");
export const degC = make("°C");
export const hz = make("Hz");
export const pct = make("%");
export const pu = make("pu");
export const customers = make("customers");

/* ── formatting ──────────────────────────────────────────────────────── */

/**
 * Value and unit as separate strings.
 *
 * Separate because the interface styles them differently everywhere — the
 * magnitude is large and tabular, the unit is small and muted — and a
 * pre-joined string would force every call site to split it back apart.
 */
export interface FormattedQuantity {
  readonly value: string;
  readonly unit: string;
}

export interface FormatOptions {
  /** Override the unit's default display precision. */
  readonly decimals?: number;
  /**
   * Group thousands with a thin space rather than a comma. On by default:
   * `1 284 610` is the convention the console uses, and a comma reads as a
   * decimal separator to half the world.
   */
  readonly group?: boolean;
}

const THIN_SPACE = "\u2009";

export function formatQuantity(q: Quantity, opts: FormatOptions = {}): FormattedQuantity {
  const spec = UNITS[q.unit];
  const decimals = opts.decimals ?? spec.decimals;
  const group = opts.group ?? true;

  if (q.unit === "pu" && q.base === undefined) {
    // A per-unit magnitude with no base cannot be converted to anything, so
    // rendering it as a bare number would be a claim we cannot support.
    return { value: "—", unit: "pu (no base)" };
  }

  const fixed = Math.abs(q.value).toFixed(decimals);
  const sign = q.value < 0 ? "−" : ""; // U+2212, not a hyphen: it aligns in tabular figures
  const [wholeRaw, fraction] = fixed.split(".");
  const whole = wholeRaw ?? "0";
  const grouped = group ? whole.replace(/\B(?=(\d{3})+(?!\d))/g, THIN_SPACE) : whole;

  return {
    value: sign + grouped + (fraction !== undefined ? `.${fraction}` : ""),
    unit: q.unit,
  };
}

/** Convenience for logs and tooltips, where one string is what is wanted. */
export function formatQuantityInline(q: Quantity, opts?: FormatOptions): string {
  const f = formatQuantity(q, opts);
  return `${f.value}\u00a0${f.unit}`;
}

/** The SI magnitude, for anything that has to compare across units. */
export function toSi(q: Quantity): number {
  const spec = UNITS[q.unit];
  return q.value * spec.toSi + spec.offset;
}

/** True when two quantities measure the same kind of thing. */
export function sameDimension(a: Quantity, b: Quantity): boolean {
  return UNITS[a.unit].dimension === UNITS[b.unit].dimension;
}
