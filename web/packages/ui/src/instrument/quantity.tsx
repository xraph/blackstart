/**
 * Rendering a measurement.
 *
 * This component takes a `Quantity`, never a `number`. That is the whole
 * design: the interface rule is that no number appears without its unit, and
 * the cheapest way to enforce a rule is to make breaking it a type error. A
 * call site that only has a float has to say what it is measuring before it
 * can display it.
 */

import type { ReactElement } from "react";
import { formatQuantity, type FormatOptions, type Quantity, type Unit } from "@blackstart/schema";
import { cn } from "../cn";

export interface QuantityProps<U extends Unit> extends FormatOptions {
  /**
   * Absent is a legitimate value, not a caller error: an island with no
   * machine attached has no frequency, and a cascade whose engine carries no
   * customer model has no interrupted count. Both are absence, not zero, so
   * the type admits `undefined` rather than pushing every call site toward
   * defaulting to 0 to satisfy a required prop.
   */
  readonly q: Quantity<U> | undefined;
  /** Hide the unit where a column header already carries it. */
  readonly unitless?: boolean;
  readonly className?: string;
  readonly unitClassName?: string;
}

export function QuantityValue<U extends Unit>({
  q,
  unitless = false,
  className,
  unitClassName,
  ...format
}: QuantityProps<U>): ReactElement {
  if (q === undefined) {
    // "not computed", never "0": a zero here would assert that the measured
    // thing was found to be nothing, which is a finding — and a calmer one
    // than the truth, which is that nothing measured it.
    return <span className={cn("font-mono tabular text-fg-3", className)}>not computed</span>;
  }

  const f = formatQuantity(q, format);
  return (
    <span className={cn("font-mono tabular", className)}>
      {f.value}
      {!unitless && <span className={cn("ml-1 text-fg-3", unitClassName)}>{f.unit}</span>}
    </span>
  );
}

/**
 * A labelled measurement, as the cascade counters show one.
 *
 * The magnitude is deliberately large and the unit small: an operator reads
 * "4 187" and already knows it is megawatts, but the unit still has to be
 * present for the screenshot to stand alone.
 */
export interface QuantityBlockProps<U extends Unit> {
  readonly label: string;
  /** See {@link QuantityProps.q} — absent is a value, not an omission. */
  readonly q: Quantity<U> | undefined;
  readonly note?: string;
  readonly decimals?: number;
  readonly className?: string;
}

export function QuantityBlock<U extends Unit>({
  label,
  q,
  note,
  decimals,
  className,
}: QuantityBlockProps<U>): ReactElement {
  const f =
    q === undefined ? undefined : formatQuantity(q, decimals === undefined ? {} : { decimals });
  return (
    <div className={cn("px-5 py-3", className)}>
      <div className="font-mono text-[9px] uppercase tracking-[0.13em] text-fg-3">{label}</div>
      {f === undefined ? (
        // Same figure, same slot, deliberately smaller: a "not computed" this
        // large would read as a headline number, and it is the opposite of one.
        <div className="mt-2 font-mono text-[13px] normal-case tracking-normal text-fg-3">
          not computed
        </div>
      ) : (
        <div className="mt-2 font-mono text-[27px] leading-none tracking-[-0.035em] tabular text-fg">
          {f.value}
          <span className="ml-1.5 text-xs tracking-normal text-fg-3">{f.unit}</span>
        </div>
      )}
      {note !== undefined && (
        <div className="mt-2 font-mono text-[9px] tabular text-fg-3">{note}</div>
      )}
    </div>
  );
}
