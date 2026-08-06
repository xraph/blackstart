/**
 * What the two top-bar selectors can be set to.
 *
 * Both are shaped for `Select` here rather than in the shell, because the
 * shell has no business knowing what a scenario is — it knows the bar carries
 * labelled values, some of which open.
 *
 * Nothing is defaulted. A selector that adopts whatever the platform listed
 * first means two operators opening the same bare URL on different days are
 * looking at different networks and have no way to tell.
 */

import { useMemo } from "react";
import { useNetworkmodelList, useScenarioScenariosList } from "@blackstart/api-client";
import type { SelectOption } from "@blackstart/ui";

import { useApiClient } from "./provider";

/** Enough to fill a dropdown; the console is not a browsing surface. */
const LIMIT = 50;

export interface ContextOptions<T extends string> {
  readonly options: readonly SelectOption<T>[];
  readonly isPending: boolean;
  readonly error: Error | null;
}

/**
 * `2026-08-02`, from an instant.
 *
 * Date only: the meta column is 9.5px beside an 11.5px name, and a scenario
 * is a thing somebody defined on a day rather than at a second.
 */
function day(at: string): string {
  return at.slice(0, 10);
}

export function useScenarioOptions(): ContextOptions<string> {
  const client = useApiClient();
  const query = useScenarioScenariosList(client, LIMIT);

  return useMemo(
    () => ({
      options: (query.data?.scenarios ?? []).map((s) => ({
        value: s.id,
        label: s.name,
        meta: day(s.createdAt),
      })),
      isPending: query.isPending,
      error: query.error,
    }),
    [query.data, query.isPending, query.error],
  );
}

export function useCaseOptions(): ContextOptions<string> {
  const client = useApiClient();
  const query = useNetworkmodelList(client, LIMIT);

  return useMemo(
    () => ({
      /* The content hash rather than the bus count, short enough to read.
       * A model is immutable and correcting one produces a new model with a
       * new hash, so the hash is what tells two builds of the same network
       * apart — which is the question a reader has when two are listed under
       * one name. */
      options: (query.data?.models ?? []).map((m) => ({
        value: m.id,
        label: m.name,
        meta: m.contentHash.slice(0, 8),
      })),
      isPending: query.isPending,
      error: query.error,
    }),
    [query.data, query.isPending, query.error],
  );
}

/**
 * The model the console is reasoning about, or null.
 *
 * Returns the whole record rather than the id because the layer stack prints
 * the name and the hash, and the screening rail needs the hash to find a run.
 * Two consumers deriving that from the id separately is how one of them ends
 * up a version behind.
 */
export function useSelectedCase(id: string | undefined) {
  const client = useApiClient();
  const query = useNetworkmodelList(client, LIMIT);

  return useMemo(() => {
    if (id === undefined) return null;
    return (query.data?.models ?? []).find((m) => m.id === id) ?? null;
  }, [query.data, id]);
}
