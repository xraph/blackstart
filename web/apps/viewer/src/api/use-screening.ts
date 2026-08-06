/**
 * The screening result behind the console's rail.
 *
 * Three hops, because there is no endpoint that goes straight from a network
 * to its latest screening: the case gives a content hash, the hash lists runs
 * newest first, and the newest run's id gives the ranked candidates.
 *
 * Every step that produces nothing is reported as *which* step, not as a
 * spinner. "No case is selected" and "this network has never been screened"
 * are different problems with different answers, and a rail that showed the
 * same emptiness for both would send the reader looking in the wrong place.
 */

import { useMemo } from "react";
import {
  useContingencyRunsList,
  useContingencyRunsRanked,
  type Candidate,
} from "@blackstart/api-client";
import { ScreeningList, sortScreening, type ScreeningSort } from "@blackstart/schema";

import { useApiClient } from "./provider";

export type ScreeningGap = "no-case" | "never-screened";

export interface Screening {
  readonly rows: ScreeningList;
  readonly isPending: boolean;
  readonly error: Error | null;
  /** Which step came up empty, or null when there are rows to show. */
  readonly gap: ScreeningGap | null;
  /** Candidates the screen could not evaluate, as the response reports it. */
  readonly unevaluated: number;
  readonly runId: string | null;
}

/**
 * Validate what the platform sent before drawing it.
 *
 * The generated client's types are structural — `screenability` is `string`
 * there — so this is where the enum and the absent-loading invariant are
 * actually enforced. A candidate that fails is dropped rather than rendered,
 * because the failure modes the schema catches all render perfectly well as
 * a row that means something false.
 */
function accepted(candidates: readonly Candidate[]): ScreeningList {
  const rows: ScreeningList = [];

  for (const candidate of candidates) {
    const parsed = ScreeningList.element.safeParse(candidate);
    if (parsed.success) rows.push(parsed.data);
  }

  return rows;
}

export function useScreening(
  modelHash: string | undefined,
  sort: ScreeningSort,
  all: boolean,
): Screening {
  const client = useApiClient();

  /* One run: the rail shows the latest screening of this network, and the
   * history belongs to the runs index. */
  const runs = useContingencyRunsList(client, modelHash, 1, { enabled: modelHash !== undefined });

  const runId = runs.data?.runs[0]?.id;

  const ranked = useContingencyRunsRanked(client, runId ?? "", all, {
    enabled: runId !== undefined,
  });

  return useMemo(() => {
    const rows = sortScreening(accepted(ranked.data?.candidates ?? []), sort);

    const gap: ScreeningGap | null =
      modelHash === undefined
        ? "no-case"
        : !runs.isPending && runId === undefined
          ? "never-screened"
          : null;

    return {
      rows,
      /* Pending only while something is genuinely outstanding. With no case
       * chosen nothing has been asked for, and reporting that as loading
       * would spin forever under a question nobody put. */
      isPending:
        modelHash !== undefined && (runs.isPending || (runId !== undefined && ranked.isPending)),
      error: runs.error ?? ranked.error,
      gap,
      unevaluated: ranked.data?.unevaluated ?? 0,
      runId: runId ?? null,
    };
  }, [
    ranked.data,
    ranked.isPending,
    ranked.error,
    runs.isPending,
    runs.error,
    runId,
    modelHash,
    sort,
  ]);
}
