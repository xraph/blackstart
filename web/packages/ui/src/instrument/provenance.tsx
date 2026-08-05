/**
 * The provenance chain.
 *
 * No result is displayed without one. Completeness is computed from the links
 * present, never taken from a flag — so the heading cannot claim a chain is
 * complete while a link underneath it is hollow.
 */

import type { ReactElement } from "react";
import {
  chainCompleteness,
  orderChain,
  type ProvenanceChain as Chain,
  type ProvenanceKind,
} from "@blackstart/schema";
import { cn } from "../cn";

const KIND_LABEL: Record<ProvenanceKind, string> = {
  dataset: "Weather dataset",
  network: "Network version",
  "feature-set": "Feature set",
  model: "Model version",
  run: "Run manifest",
  commit: "Code commit",
};

export function ProvenanceChainView({ chain }: { readonly chain: Chain }): ReactElement {
  const status = chainCompleteness(chain);
  const links = orderChain(chain);

  return (
    <div>
      <div className="mb-2.5 flex items-center gap-2 font-mono text-[9px] uppercase tracking-[0.13em] text-fg-3">
        Provenance
        <span className="h-px flex-1 bg-line" />
        <span
          className={cn("normal-case tracking-normal", status.complete ? "text-ok" : "text-risk-3")}
        >
          {status.complete
            ? "chain complete"
            : status.missing.length > 0
              ? `${status.missing.length} link${status.missing.length === 1 ? "" : "s"} missing`
              : "unverified"}
        </span>
      </div>

      <ol className="relative m-0 list-none p-0 pl-4">
        <span aria-hidden="true" className="absolute bottom-1.5 left-1 top-1.5 w-px bg-line-2" />
        {links.map((link) => (
          <li key={link.kind} className="relative pb-2.5 last:pb-0">
            <span
              aria-hidden="true"
              className={cn(
                "absolute -left-[14px] top-1 size-[7px] rounded-full border",
                link.verified ? "border-ok bg-ok" : "border-line-3 bg-panel",
              )}
            />
            <div className="font-mono text-[8.5px] uppercase tracking-[0.1em] text-fg-3">
              {KIND_LABEL[link.kind]}
            </div>
            <div className="mt-0.5 flex items-center gap-[7px] font-mono text-[10px] tabular text-fg">
              {link.label}
              {link.digest !== undefined && <span className="text-fg-3">{link.digest}</span>}
              {!link.verified && <span className="text-risk-3">unverified</span>}
            </div>
          </li>
        ))}

        {/* A missing link renders as a gap with a name, never as nothing. */}
        {status.missing.map((kind) => (
          <li key={kind} className="relative pb-2.5 last:pb-0">
            <span
              aria-hidden="true"
              className="absolute -left-[14px] top-1 size-[7px] rounded-full border border-dashed border-risk-3"
            />
            <div className="font-mono text-[8.5px] uppercase tracking-[0.1em] text-fg-3">
              {KIND_LABEL[kind]}
            </div>
            <div className="mt-0.5 font-mono text-[10px] text-risk-3">not recorded</div>
          </li>
        ))}
      </ol>
    </div>
  );
}
