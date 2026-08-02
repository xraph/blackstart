/**
 * The element inspector.
 *
 * Replaces the rail rather than opening as a modal. A modal would cover the
 * globe, and the spatial context — where this span sits relative to the storm
 * and to everything else at risk — is most of what the reader is using to
 * decide. Losing it to show a panel is a bad trade.
 *
 * The payload arrives as one `ElementExplanation` because the panel is useless
 * partially filled: a risk figure without the provenance beside it is exactly
 * what the project's rules forbid displaying.
 */

import type { ReactElement, ReactNode } from "react";
import { formatQuantityInline, type ElementExplanation } from "@blackstart/schema";
import { cn } from "../cn";
import { QuantityValue } from "../instrument/quantity";
import { AliasTable, TypeIdChip } from "../instrument/identity";
import { ProbabilityValue, RiskWaterfall } from "../instrument/risk";
import { Sparkline } from "../instrument/sparkline";
import { ProvenanceChainView } from "../instrument/provenance";

export function ElementInspector({
  explanation,
  onClose,
}: {
  readonly explanation: ElementExplanation;
  readonly onClose?: () => void;
}): ReactElement {
  const { element, probability, rank, totalRanked, decomposition, conditions, analogues, counterfactual } =
    explanation;

  return (
    <aside className="flex min-h-0 w-[480px] shrink-0 flex-col border-l border-line bg-panel">
      <header className="shrink-0 border-b border-line px-3.5 pb-2.5 pt-2.5">
        <div className="flex items-center gap-2 font-mono text-[9px] uppercase tracking-[0.13em] text-fg-3">
          <span>Transmission line</span>
          <ProbabilityValue p={probability} className="text-[9px]" />
          <button
            type="button"
            onClick={onClose}
            aria-label="Close inspector"
            className="ml-auto text-xs text-fg-3 hover:text-fg focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2"
          >
            ✕
          </button>
        </div>
        <h2 className="m-0 mt-1.5 text-[15px] font-semibold tracking-[-0.018em] text-fg">
          {element.name}
        </h2>
        <div className="mt-1.5 flex items-center gap-1.5">
          <TypeIdChip id={element.id} full />
          {rank !== undefined && totalRanked !== undefined && (
            <span className="ml-auto font-mono text-[10px] text-fg-3">
              rank {rank} / {totalRanked}
            </span>
          )}
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <Section title="Identity &amp; aliases">
          <dl className="grid grid-cols-[96px_1fr] gap-x-2.5 gap-y-[3px] font-mono text-[10px] tabular">
            <dt className="text-fg-3">conductor</dt>
            <dd className="m-0 text-fg">
              {element.conductor} · bundled {element.bundle}
            </dd>
            <dt className="text-fg-3">length</dt>
            <dd className="m-0 text-fg">
              <QuantityValue q={element.length} className="text-[10px]" /> · {element.spanCount} spans ·{" "}
              {element.structureCount} structures
            </dd>
            <dt className="text-fg-3">rating</dt>
            <dd className="m-0 text-fg">
              normal {formatQuantityInline(element.rating.normal)} · emerg{" "}
              {formatQuantityInline(element.rating.emergency)} @{" "}
              {formatQuantityInline(element.rating.ambient)}
            </dd>
          </dl>
          <div className="mt-2 border-t border-line-t pt-2">
            <AliasTable aliases={element.aliases} />
          </div>
        </Section>

        <Section title="Risk decomposition" meta={decomposition.model}>
          <RiskWaterfall decomposition={decomposition} />
        </Section>

        <Section title="Driving conditions — span max">
          <div className="grid grid-cols-4 gap-px overflow-hidden rounded-[2px] border border-line bg-line">
            {conditions.map((c) => (
              <div key={c.label} className="bg-panel-2 px-[7px] py-1.5">
                <div className="font-mono text-[8.5px] tracking-[0.06em] text-fg-3">{c.label}</div>
                <div className="mt-[3px] font-mono text-sm tracking-[-0.02em] tabular text-fg">
                  {c.current.value}
                  <span className="ml-0.5 text-[9px] text-fg-3">{c.current.unit}</span>
                </div>
                <Sparkline
                  points={c.points}
                  width={80}
                  height={16}
                  colour={`var(--bs-risk-${c.bucket})`}
                  className="mt-1 w-full"
                  label={`${c.label} over the forecast horizon`}
                />
              </div>
            ))}
          </div>
        </Section>

        <Section title="Nearest historical analogues">
          {analogues.map((a) => (
            <div
              key={a.date}
              className="flex h-[26px] items-center gap-2.5 border-b border-line-t font-mono text-[10px] tabular last:border-b-0"
            >
              <span className="w-[74px] shrink-0 text-fg-2">{a.date}</span>
              <span className="w-[34px] shrink-0 text-fg-3">{a.similarity.toFixed(2)}</span>
              <span className="flex-1 truncate text-fg">{a.event}</span>
              <span className="text-fg-3">{a.outcome}</span>
            </div>
          ))}
        </Section>

        <Section title="Counterfactual">
          <p className="m-0 rounded-r-[2px] border-l-2 border-fg-3 bg-panel-2 px-2.5 py-2 text-[11.5px] leading-snug text-fg-2">
            Risk falls below the action threshold if {counterfactual.feature} stays under{" "}
            <b className="font-mono font-semibold tabular text-fg">
              {formatQuantityInline(counterfactual.threshold)}
            </b>{" "}
            — currently forecast at{" "}
            <b className="font-mono font-semibold tabular text-fg">
              {formatQuantityInline(counterfactual.current)}
            </b>
            .{" "}
            {counterfactual.requiresMultiple
              ? "No other single feature reaches the threshold alone."
              : null}
          </p>
        </Section>

        <Section title="" last>
          <ProvenanceChainView chain={element.provenance} />
        </Section>
      </div>
    </aside>
  );
}

function Section({
  title,
  meta,
  last = false,
  children,
}: {
  readonly title: string;
  readonly meta?: string;
  readonly last?: boolean;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <section className={cn("px-3.5 pb-3.5 pt-2.5", !last && "border-b border-line")}>
      {title !== "" && (
        <h3 className="mb-2.5 flex items-center gap-2 font-mono text-[9px] font-normal uppercase tracking-[0.13em] text-fg-3">
          {title}
          <span aria-hidden="true" className="h-px flex-1 bg-line" />
          {meta !== undefined && <span className="normal-case tracking-normal">{meta}</span>}
        </h3>
      )}
      {children}
    </section>
  );
}
