/**
 * The platform's provenance trace, as the console's chain.
 *
 * The API returns a trace shaped for the platform — inputs with roles, a
 * producing model, the run, and whether it reproduces. The console renders a
 * chain of links. This is the join, and it lives here rather than in a
 * component because it is a data-shape decision, not a rendering one.
 *
 * Nothing is invented. A link the trace does not carry is simply absent, and
 * the chain view renders the gap by name — which is the whole reason
 * completeness is computed from the links rather than asserted by a flag.
 */

import type { traceResponse, TracedInput } from "@blackstart/api-client";
import { ProvenanceChain, type ProvenanceKind } from "@blackstart/schema";

/**
 * Which link a traced input becomes.
 *
 * The platform tags each input with a role. A network case is the network
 * link; everything else is a dataset, of which there may legitimately be
 * several — a weather cycle, a terrain tile set, a land-cover raster.
 */
function kindOf(input: TracedInput): ProvenanceKind {
  const role = input.role.toLowerCase();

  if (role.includes("network") || role.includes("case") || role.includes("model")) {
    return "network";
  }

  return "dataset";
}

/** Shortens a content hash for display without losing its identity. */
function abbreviateHash(hash: string): string | undefined {
  if (hash === "") return undefined;

  return hash.length <= 12 ? hash : `${hash.slice(0, 8)}…${hash.slice(-4)}`;
}

/**
 * Builds the chain a run is citable through.
 *
 * `reproducible` is the platform's own verdict on the whole trace, so it marks
 * the run and commit links verified. An input is verified when it carries a
 * content hash: a dataset nobody hashed is present and unchecked, and the
 * chain view draws that difference.
 */
export function chainOfTrace(trace: traceResponse): ProvenanceChain {
  const links: ProvenanceChain = [];
  const seenNetwork = new Set<string>();

  for (const input of trace.inputs) {
    const kind = kindOf(input);

    // Only one network link is permitted. A second traced input claiming the
    // network role is a fact about the trace worth keeping rather than a
    // reason to drop it, so it degrades to a dataset link.
    const resolved = kind === "network" && seenNetwork.size > 0 ? "dataset" : kind;
    if (resolved === "network") seenNetwork.add(input.dataset.id);

    const digest = abbreviateHash(input.dataset.contentHash);

    links.push({
      kind: resolved,
      label: input.dataset.url === "" ? input.dataset.id : input.dataset.url,
      ref: input.dataset.id,
      ...(digest === undefined ? {} : { digest }),
      verified: input.dataset.contentHash !== "",
    });
  }

  if (trace.producer.id !== "") {
    links.push({
      kind: "model",
      label: `${trace.producer.name} ${trace.producer.version}`.trim(),
      ref: trace.producer.id,
      verified: true,
    });
  }

  if (trace.run.id !== "") {
    links.push({
      kind: "run",
      label: `solver ${trace.run.solverVersion}`,
      ref: trace.run.id,
      ...(trace.run.modelHash === "" ? {} : { digest: abbreviateHash(trace.run.modelHash) }),
      verified: trace.reproducible,
    });
  }

  if (trace.producer.codeCommit !== "") {
    links.push({
      kind: "commit",
      label: trace.reproducible
        ? `${trace.producer.codeCommit} · reproducible`
        : trace.producer.codeCommit,
      ref: trace.producer.codeCommit,
      verified: trace.reproducible,
    });
  }

  return ProvenanceChain.parse(links);
}
