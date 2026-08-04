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
import {
  formatQuantity,
  type FormatOptions,
  type Quantity,
  type Unit,
} from "@blackstart/schema";
import { cn } from "../cn";

/**
 * What a missing measurement looks like.
 *
 * An em dash rather than a zero, and that is the whole point. The cascade
 * schema makes frequency, nadir and the customer counters optional because an
 * island with no machine attached has no frequency to report — absent, not
 * zero. Rendering a 0 would state a measurement nobody made, and "0.0 Hz" on
 * an island reads as a total collapse rather than as silence.
 */
const ABSENT = "\u2014";

export interface QuantityProps<U extends Unit> extends FormatOptions {
  /** Undefined where the source had nothing to report. See ABSENT. */
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
    return (
      <span className={cn("font-mono tabular text-fg-3", className)}>
        {ABSENT}
        <span className="sr-only">not reported</span>
      </span>
    );
  }

  const f = formatQuantity(q, format);
  return (
    <span className={cn("font-mono tabular", className)}>
      {f.value}
      {!unitless && (
        <span className={cn("ml-1 text-fg-3", unitClassName)}>{f.unit}</span>
      )}
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
  /** Undefined where the source had nothing to report. See ABSENT. */
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
    q === undefined
      ? { value: ABSENT, unit: "" }
      : formatQuantity(q, decimals === undefined ? {} : { decimals });
  return (
    <div className={cn("px-5 py-3", className)}>
      <div className="font-mono text-[9px] uppercase tracking-[0.13em] text-fg-3">
        {label}
      </div>
      <div className="mt-2 font-mono text-[27px] leading-none tracking-[-0.035em] tabular text-fg">
        {f.value}
        <span className="ml-1.5 text-xs tracking-normal text-fg-3">{f.unit}</span>
      </div>
      {note !== undefined && (
        <div className="mt-2 font-mono text-[9px] tabular text-fg-3">{note}</div>
      )}
    </div>
  );
}
