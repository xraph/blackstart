/**
 * A legend for a measured quantity.
 *
 * The console's rule, applied to something the platform can actually serve:
 * print the class breaks the buckets are quantised to, in the unit they are
 * measured in. A ramp without numbers says one thing is worse than another and
 * nothing about whether either is worth acting on.
 *
 * This replaces the risk legend on the console. `RiskRamp` prints a
 * probability, and no model in this repository produces one — `risk` is Phase
 * 6. A legend advertising a calibrated P(fail) over a platform that publishes
 * amperes is the one kind of error a console like this cannot afford.
 */

import type { ReactElement } from "react";
import { cn } from "../cn";
import { bucketBg } from "../lib/palette";

export interface MeasureRampProps {
  readonly label: string;
  /** The unit the breaks are in, printed beside them. */
  readonly unit: string;
  /** Four ascending breaks, giving five buckets. */
  readonly breaks: readonly number[];
  /** Which end of the scale is the severe one. */
  readonly severity: "high" | "low";
  readonly className?: string;
}

export function MeasureRamp({
  label,
  unit,
  breaks,
  severity,
  className,
}: MeasureRampProps): ReactElement {
  /* The axis always ascends left to right, because that is what an axis does.
   * The colours follow the encoding instead: for a rating, which is headroom,
   * the small values are the severe ones and the red belongs at the left. */
  const buckets = [0, 1, 2, 3, 4];
  const order = severity === "high" ? buckets : [...buckets].reverse();

  return (
    <div className={className}>
      <div className="flex h-[9px] overflow-hidden rounded-[2px]">
        {order.map((b) => (
          <i key={b} data-bucket={b} className={cn("flex-1", bucketBg(b as 0 | 1 | 2 | 3 | 4))} />
        ))}
      </div>

      <div className="mt-1.5 flex justify-between font-mono text-[9px] tabular text-fg-3">
        {breaks.map((b) => (
          <span key={b}>{b}</span>
        ))}
        <span className="text-fg-2">{unit}</span>
      </div>

      <div className="mt-1.5 flex items-center gap-1.5 font-mono text-[9px] text-fg-3">
        {/* Outside the ramp, not a sixth bucket. "Not computed" is not a point
            on a scale of severity, and putting it on one would invite the
            reader to compare it with the values that are. */}
        <i className="size-[7px] shrink-0 rounded-[1px] bg-fg-4 opacity-60" />
        <span>unrated — no conductor assigned</span>
        <span className="ml-auto text-fg-2">{label}</span>
      </div>
    </div>
  );
}
