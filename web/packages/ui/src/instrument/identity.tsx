/**
 * Identifiers on screen.
 *
 * Always monospace, always copyable. An identifier a reviewer cannot copy out
 * of a screenshot is decoration — the point of showing it is that someone can
 * paste it into `bsctl` and get the same record back.
 */

import { useCallback, useState, type ReactElement } from "react";
import { abbreviateTypeId, prefixOf, TYPE_PREFIXES, type Alias } from "@blackstart/schema";
import { cn } from "../cn";

export interface TypeIdChipProps {
  readonly id: string;
  /** Show the full identifier rather than the abbreviated tail. */
  readonly full?: boolean;
  readonly className?: string;
}

export function TypeIdChip({ id, full = false, className }: TypeIdChipProps): ReactElement {
  const [copied, setCopied] = useState(false);
  const prefix = prefixOf(id);

  const copy = useCallback(() => {
    void navigator.clipboard?.writeText(id).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    });
  }, [id]);

  return (
    <span className={cn("inline-flex items-center gap-1.5", className)}>
      <span
        className="font-mono text-[10px] text-fg-3"
        title={prefix === null ? id : `${TYPE_PREFIXES[prefix]} — ${id}`}
      >
        {full ? id : abbreviateTypeId(id)}
      </span>
      <button
        type="button"
        onClick={copy}
        className="touch-target rounded-[2px] border border-line-2 px-1 text-[8.5px] tracking-[0.06em] text-fg-3 hover:text-fg focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2"
        aria-label={`Copy identifier ${id}`}
      >
        {copied ? "COPIED" : "COPY"}
      </button>
    </span>
  );
}

/**
 * The alias table.
 *
 * Every external identifier that resolved to this record, with what its source
 * is trusted for. Reconciliation has to stay auditable and reversible, so this
 * never collapses to "and 3 others".
 */
export function AliasTable({ aliases }: { readonly aliases: readonly Alias[] }): ReactElement {
  return (
    <dl className="grid grid-cols-[96px_1fr] gap-x-2.5 gap-y-[3px] font-mono text-[10px] tabular">
      {aliases.map((a) => (
        <div key={`${a.system}:${a.externalId}`} className="contents">
          <dt className="text-fg-3">{a.system}</dt>
          <dd className="m-0 text-fg">
            {a.externalId} <span className="text-fg-3">({a.authority})</span>
          </dd>
        </div>
      ))}
    </dl>
  );
}
