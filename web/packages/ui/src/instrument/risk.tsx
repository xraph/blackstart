/**
 * Risk display primitives.
 *
 * The legend prints the actual class breaks rather than an unlabelled
 * gradient bar, because a ramp without numbers tells an operator that
 * something is worse than something else and nothing about whether either is
 * worth acting on.
 */

import type { ReactElement } from "react";
import {
  ACTION_THRESHOLD,
  RISK_BREAKS,
  RISK_BUCKET_LABELS,
  riskBucket,
  type Probability,
  type RiskDecomposition,
} from "@blackstart/schema";
import { cn } from "../cn";
import { bucketBg, riskBg, riskText } from "../lib/palette";

/** A probability, coloured by band, with the band named for screen readers. */
export function ProbabilityValue({
  p,
  className,
}: {
  readonly p: Probability;
  readonly className?: string;
}): ReactElement {
  const bucket = riskBucket(p);
  return (
    <span
      className={cn("font-mono tabular tracking-[-0.02em]", riskText(p), className)}
      title={`${RISK_BUCKET_LABELS[bucket]}${p >= ACTION_THRESHOLD ? " — above action threshold" : ""}`}
    >
      {p.toFixed(3)}
    </span>
  );
}

/** The 2 px band that runs down the left of a contingency row. */
export function RiskBar({ p }: { readonly p: Probability }): ReactElement {
  return <span className={cn("h-[34px] w-[2px] shrink-0 rounded-[1px]", riskBg(p))} />;
}

/**
 * The ramp legend, with the breaks the buckets are actually quantised to.
 */
export function RiskRamp({ className }: { readonly className?: string }): ReactElement {
  return (
    <div className={className}>
      <div className="flex h-[9px] overflow-hidden rounded-[2px]">
        {[0, 1, 2, 3, 4].map((b) => (
          <i key={b} className={cn("flex-1", bucketBg(b as 0 | 1 | 2 | 3 | 4))} />
        ))}
      </div>
      <div className="mt-1.5 flex justify-between font-mono text-[9px] tabular text-fg-3">
        <span>0.00</span>
        {RISK_BREAKS.map((b) => (
          <span key={b}>{b.toFixed(2).replace(/^0/, "")}</span>
        ))}
        <span>0.50+</span>
      </div>
    </div>
  );
}

/**
 * The decomposition waterfall.
 *
 * Contributions are laid out end to end so the bar's left edge is where the
 * previous one finished — the reader follows the accumulation rather than
 * comparing five bars from a shared baseline, which is what a bar chart would
 * show and is a different claim.
 *
 * The schema guarantees these sum to the total, so the layout arithmetic here
 * cannot drift from the number printed at the bottom.
 */
export function RiskWaterfall({
  decomposition,
}: {
  readonly decomposition: RiskDecomposition;
}): ReactElement {
  const { baseline, contributions, total } = decomposition;
  const scale = total === 0 ? 1 : total;

  let cursor = baseline / scale;
  const rows = contributions.map((c) => {
    const width = Math.abs(c.delta) / scale;
    const left = c.delta >= 0 ? cursor : cursor - width;
    cursor += c.delta / scale;
    return { ...c, left, width };
  });

  return (
    <div className="flex flex-col">
      <WaterfallRow
        label="baseline hazard"
        left={0}
        width={baseline / scale}
        tone="bg-fg-4"
        value={baseline.toFixed(3)}
      />
      {rows.map((r) => (
        <WaterfallRow
          key={r.feature}
          label={`${r.delta >= 0 ? "+" : "−"} ${r.feature}`}
          left={r.left}
          width={r.width}
          tone={bucketBg(riskBucket(Math.abs(r.delta) * 2))}
          value={`${r.delta >= 0 ? "+" : "−"}${Math.abs(r.delta).toFixed(3)}`}
          title={r.evidence}
        />
      ))}
      <div className="mt-1.5 flex h-[27px] items-center gap-2 border-t border-line-2 pt-1.5">
        <span className="w-[104px] shrink-0 text-[10px] font-semibold text-fg">P(fail)</span>
        <span className="relative h-[11px] flex-1 rounded-[1px] border border-line-t bg-panel-2">
          <i
            className="absolute inset-y-0 left-0 w-full rounded-[1px]"
            style={{
              background: "linear-gradient(90deg, var(--bs-fg-4), var(--bs-risk-4))",
            }}
          />
        </span>
        <span className="w-11 shrink-0 text-right font-mono text-[13px] tracking-[-0.02em] tabular text-fg">
          {total.toFixed(3)}
        </span>
      </div>
    </div>
  );
}

function WaterfallRow({
  label,
  left,
  width,
  tone,
  value,
  title,
}: {
  readonly label: string;
  readonly left: number;
  readonly width: number;
  readonly tone: string;
  readonly value: string;
  readonly title?: string | undefined;
}): ReactElement {
  return (
    <div className="flex h-[22px] items-center gap-2" title={title}>
      <span className="w-[104px] shrink-0 text-[10px] text-fg-2">{label}</span>
      <span className="relative h-[11px] flex-1 rounded-[1px] border border-line-t bg-panel-2">
        <i
          className={cn("absolute inset-y-0 rounded-[1px]", tone)}
          style={{
            left: `${Math.max(0, left) * 100}%`,
            width: `${Math.max(0.01, width) * 100}%`,
          }}
        />
      </span>
      <span className="w-11 shrink-0 text-right font-mono text-[10px] tabular text-fg">
        {value}
      </span>
    </div>
  );
}
