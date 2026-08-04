/**
 * Provenance chains.
 *
 * The project's hard rule is that no result is displayed in the client without
 * a provenance link. That is enforced here rather than by convention: the
 * payload schemas below require a chain, and completeness is *computed* from
 * the links present rather than asserted by a flag the server could get wrong.
 *
 * A chain with a missing link still renders — hollow, with the gap named. An
 * interface that silently hides an incomplete chain is worse than one that
 * shows the hole, because the reviewer cannot tell the difference between
 * "verified" and "not checked".
 */

import { z } from "zod";

/**
 * The links a result is traceable through, in the order they are shown.
 *
 * Order is causal: the dataset and the network version are inputs, the feature
 * set derives from them, the model consumes the features, the run invokes the
 * model, and the commit is the code that did all of it.
 */
export const PROVENANCE_KINDS = [
  "dataset",
  "network",
  "feature-set",
  "model",
  "run",
  "commit",
] as const;

export type ProvenanceKind = (typeof PROVENANCE_KINDS)[number];

/**
 * The links without which a number is not citable.
 *
 * `feature-set` is excluded because a raw observation display — a gust reading
 * straight off HRRR — legitimately has none, and demanding one would push
 * callers toward inventing a placeholder, which is the failure mode this is
 * trying to prevent.
 */
export const REQUIRED_KINDS: readonly ProvenanceKind[] = ["run", "commit"];

/**
 * The kinds that count as an input.
 *
 * A result must be traceable to at least one of these, but not to any
 * particular one: a contingency sweep reads a network case and no weather,
 * while a hazard score reads a weather cycle and no case. Requiring "dataset"
 * specifically marked the first of those incomplete, which the platform's own
 * first trace demonstrated.
 */
export const INPUT_KINDS: readonly ProvenanceKind[] = ["dataset", "network"];

export const ProvenanceLink = z.object({
  kind: z.enum(PROVENANCE_KINDS),
  /** What it is, in the operator's words: `HRRR conus 18Z f04`. */
  label: z.string().min(1),
  /** The version or identifier that pins it. */
  ref: z.string().min(1),
  /** Content hash where the source has one. Absent is honest; empty is not. */
  digest: z.string().min(1).optional(),
  /**
   * Whether the link was checked, not whether it was recorded. A dataset whose
   * hash did not match what the manifest claimed is present and unverified.
   *
   * Defaults to false rather than being required: a caller that cites a link
   * without stating whether it checked it has not checked it, and defaulting
   * to true would let that omission pass as verification.
   */
  verified: z.boolean().default(false),
});
export type ProvenanceLink = z.infer<typeof ProvenanceLink>;

/**
 * The kinds of which a result has exactly one.
 *
 * Two models, two runs or two commits are two answers to "what produced this",
 * which is not a display problem — it is a corrupt manifest.
 *
 * Datasets are deliberately absent from this list. A run legitimately consumes
 * several inputs — a network case and a weather cycle and a terrain tile set —
 * and the platform's own trace returns them as an array with a role on each.
 * The original rule forbade that, and the first real trace said so.
 */
const SINGULAR_KINDS: readonly ProvenanceKind[] = ["network", "feature-set", "model", "run", "commit"];

export const ProvenanceChain = z.array(ProvenanceLink).superRefine((links, ctx) => {
  const seen = new Set<ProvenanceKind>();

  for (const link of links) {
    if (!SINGULAR_KINDS.includes(link.kind)) continue;

    if (seen.has(link.kind)) {
      ctx.addIssue({
        code: "custom",
        message: `two ${link.kind} links: a result has exactly one`,
        path: ["kind"],
      });
    }

    seen.add(link.kind);
  }
});
export type ProvenanceChain = z.infer<typeof ProvenanceChain>;

export interface ChainCompleteness {
  readonly complete: boolean;
  readonly missing: readonly ProvenanceKind[];
  readonly unverified: readonly ProvenanceKind[];
}

/**
 * Compute, never assert.
 *
 * A chain is complete when every required kind is present *and* verified. The
 * component renders "chain complete" from this result, so the badge cannot
 * disagree with the links underneath it.
 */
export function chainCompleteness(chain: ProvenanceChain): ChainCompleteness {
  const byKind = new Map(chain.map((l) => [l.kind, l]));
  const missing = REQUIRED_KINDS.filter((k) => !byKind.has(k));

  // At least one input, of whichever kind this result actually consumed.
  if (!INPUT_KINDS.some((k) => byKind.has(k))) {
    missing.push("dataset");
  }
  const unverified = PROVENANCE_KINDS.filter((k) => {
    const link = byKind.get(k);
    return link !== undefined && !link.verified;
  });
  return {
    complete: missing.length === 0 && unverified.length === 0,
    missing,
    unverified,
  };
}

/** Links in causal order, whatever order they arrived in. */
export function orderChain(chain: ProvenanceChain): ProvenanceChain {
  return [...chain].sort(
    (a, b) => PROVENANCE_KINDS.indexOf(a.kind) - PROVENANCE_KINDS.indexOf(b.kind),
  );
}

/**
 * Anything the client displays as a result carries one of these.
 *
 * Spread into a payload schema with `...cited.shape` so the requirement is
 * structural and a new payload cannot quietly omit it.
 */
export const cited = z.object({ provenance: ProvenanceChain });
