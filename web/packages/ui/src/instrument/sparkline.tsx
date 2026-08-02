/**
 * A sparkline, drawn with the same care as a full chart.
 *
 * Area fill under the line, a baseline rule, and an emphasised endpoint. The
 * endpoint matters most: in a contingency rail the reader is scanning for
 * where risk *ends up* across the forecast horizon, not the shape it took
 * getting there.
 */

import type { ReactElement } from "react";
import { riskVar } from "../lib/palette";
import { cn } from "../cn";

export interface SparklineProps {
  readonly points: readonly number[];
  /** Fixed scale, so rows are comparable. Defaults to the series maximum. */
  readonly max?: number;
  readonly width?: number;
  readonly height?: number;
  /** Colour source. Defaults to the risk ramp at the final value. */
  readonly colour?: string;
  readonly className?: string;
  readonly label?: string;
}

export function Sparkline({
  points,
  max,
  width = 44,
  height = 20,
  colour,
  className,
  label,
}: SparklineProps): ReactElement | null {
  if (points.length < 2) return null;

  const last = points[points.length - 1] ?? 0;
  const ceiling = max ?? Math.max(...points, Number.EPSILON);
  const stroke = colour ?? riskVar(last);

  const x = (i: number) => 1 + (i / (points.length - 1)) * (width - 2);
  const y = (v: number) => height - 2 - Math.min(1, v / ceiling) * (height - 4);

  const d = points.map((v, i) => `${i === 0 ? "M" : "L"}${x(i).toFixed(1)} ${y(v).toFixed(1)}`).join(" ");
  const area = `${d} L${x(points.length - 1).toFixed(1)} ${height - 1} L${x(0).toFixed(1)} ${height - 1} Z`;

  return (
    <svg
      className={cn("shrink-0", className)}
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label={label ?? `trend ending at ${last.toFixed(3)}`}
    >
      <line
        x1="1"
        y1={height - 1}
        x2={width - 1}
        y2={height - 1}
        stroke="var(--bs-line-2)"
        strokeWidth="1"
      />
      <path d={area} fill={stroke} opacity="0.14" />
      <path d={d} fill="none" stroke={stroke} strokeWidth="1.2" strokeLinejoin="round" />
      <circle cx={x(points.length - 1)} cy={y(last)} r="1.7" fill={stroke} />
    </svg>
  );
}

/**
 * A frequency trace, with the load-shedding threshold drawn in.
 *
 * The 59.5 Hz rule is the whole reason to look at this chart — an island's
 * trace matters relative to where under-frequency shedding fires, not in the
 * abstract.
 */
export function FrequencyTrace({
  points,
  colour,
  thresholdHz = 59.5,
  loHz = 56.8,
  hiHz = 60.6,
}: {
  readonly points: readonly number[];
  readonly colour: string;
  readonly thresholdHz?: number;
  readonly loHz?: number;
  readonly hiHz?: number;
}): ReactElement {
  const y = (hz: number) => 42 - ((hz - loHz) / (hiHz - loHz)) * 40;
  const d = points
    .map((v, i) => `${i === 0 ? "M" : "L"}${((i / (points.length - 1)) * 100).toFixed(1)} ${y(v).toFixed(1)}`)
    .join(" ");

  return (
    <svg className="mt-1 block h-11 w-full" viewBox="0 0 100 44" preserveAspectRatio="none" role="img"
      aria-label={`frequency trace, nadir ${Math.min(...points).toFixed(2)} hertz`}>
      <line
        x1="0"
        y1={y(thresholdHz)}
        x2="100"
        y2={y(thresholdHz)}
        stroke="var(--bs-line-2)"
        strokeWidth="0.6"
        strokeDasharray="2 2"
      />
      <path d={d} fill="none" stroke={colour} strokeWidth="1.3" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}
