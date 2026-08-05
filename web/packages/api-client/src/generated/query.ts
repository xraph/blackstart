/**
 * TanStack Query hooks over the generated client.
 *
 * Generated. Every hook calls a method on the REST client rather than issuing
 * its own request, so there is one description of the API and one place a
 * change to it lands.
 *
 * The client is passed in rather than read from a module-level singleton or a
 * context this file invents. A generated file should not decide how an
 * application provides its dependencies, and an explicit argument keeps these
 * usable from a test without a provider tree.
 */

import {
  useMutation,
  useQuery,
  type UseMutationOptions,
  type UseQueryOptions,
} from '@tanstack/react-query';
import type { RESTClient } from './rest';
import * as types from './types';

/**
 * Cache keys.
 *
 * Every parameter the method accepts is part of its key. That is not
 * thoroughness for its own sake: a key that omits a parameter serves one
 * request's cached answer to a different request. Where an API is versioned
 * along more than one axis — a valid time and a knowledge time, say — a key
 * carrying only the first will happily return what is true now to a caller
 * that asked what was known then, and the answer looks entirely plausible.
 *
 * Keys are prefixed by their access path, so invalidating a whole group is
 * `queryClient.invalidateQueries({ queryKey: ['networkmodel'] })`.
 */
export const queryKeys = {
  cascadeRunsExtent: (id: string, halfWidthM?: number | undefined) =>
    ['cascade', 'runs', 'extent', { id, halfWidthM }] as const,
  cascadeRunsGet: (id: string) =>
    ['cascade', 'runs', 'get', { id }] as const,
  cascadeRunsIslands: (id: string) =>
    ['cascade', 'runs', 'islands', { id }] as const,
  cascadeRunsList: (modelHash?: string, limit?: number | undefined) =>
    ['cascade', 'runs', 'list', { modelHash, limit }] as const,
  contingencyRunsGet: (id: string) =>
    ['contingency', 'runs', 'get', { id }] as const,
  contingencyRunsList: (modelHash?: string, limit?: number | undefined) =>
    ['contingency', 'runs', 'list', { modelHash, limit }] as const,
  contingencyRunsRanked: (id: string, all?: boolean | undefined) =>
    ['contingency', 'runs', 'ranked', { id, all }] as const,
  datasourceFeedsGet: (id: string, validAt?: string | undefined, knownAt?: string | undefined) =>
    ['datasource', 'feeds', 'get', { id, validAt, knownAt }] as const,
  datasourceFeedsList: (id: string, limit?: number | undefined, after?: string | undefined, validAt?: string | undefined, knownAt?: string | undefined) =>
    ['datasource', 'feeds', 'list', { id, limit, after, validAt, knownAt }] as const,
  datasourceSourcesGet: (id: string, validAt?: string | undefined, knownAt?: string | undefined) =>
    ['datasource', 'sources', 'get', { id, validAt, knownAt }] as const,
  datasourceSourcesList: (kind?: string | undefined, slug?: string | undefined, limit?: number | undefined, after?: string | undefined, validAt?: string | undefined, knownAt?: string | undefined) =>
    ['datasource', 'sources', 'list', { kind, slug, limit, after, validAt, knownAt }] as const,
  geoBranchesRoute: (id: string) =>
    ['geo', 'branches', 'route', { id }] as const,
  geoCircuitsGet: (id: string) =>
    ['geo', 'circuits', 'get', { id }] as const,
  geoCircuitsList: (limit?: number | undefined) =>
    ['geo', 'circuits', 'list', { limit }] as const,
  geoCircuitsRoute: (id: string) =>
    ['geo', 'circuits', 'route', { id }] as const,
  geoRoutesGet: (id: string) =>
    ['geo', 'routes', 'get', { id }] as const,
  geoRoutesList: (limit?: number | undefined) =>
    ['geo', 'routes', 'list', { limit }] as const,
  geoRoutesSpans: (id: string) =>
    ['geo', 'routes', 'spans', { id }] as const,
  hazardAssignmentsList: (limit?: number | undefined, after?: string | undefined) =>
    ['hazard', 'assignments', 'list', { limit, after }] as const,
  hazardComputationsList: (limit?: number | undefined, after?: string | undefined) =>
    ['hazard', 'computations', 'list', { limit, after }] as const,
  hazardConductorsList: () =>
    ['hazard', 'conductors', 'list'] as const,
  hazardSpansLoading: (id: string, issued?: string, from?: string, to?: string) =>
    ['hazard', 'spans', 'loading', { id, issued, from, to }] as const,
  hazardSpansRating: (id: string, issued?: string, from?: string, to?: string) =>
    ['hazard', 'spans', 'rating', { id, issued, from, to }] as const,
  networkmodelEncoding: (id: string) =>
    ['networkmodel', 'encoding', { id }] as const,
  networkmodelGet: (id: string) =>
    ['networkmodel', 'get', { id }] as const,
  networkmodelList: (limit?: number | undefined) =>
    ['networkmodel', 'list', { limit }] as const,
  powerflowSolutionsGet: (id: string) =>
    ['powerflow', 'solutions', 'get', { id }] as const,
  powerflowSolutionsList: (modelHash?: string, limit?: number | undefined) =>
    ['powerflow', 'solutions', 'list', { modelHash, limit }] as const,
  powerflowSolutionsViolations: (id: string, threshold?: number | undefined) =>
    ['powerflow', 'solutions', 'violations', { id, threshold }] as const,
  provenanceDatasetsGet: (id: string) =>
    ['provenance', 'datasets', 'get', { id }] as const,
  provenanceModelsGet: (id: string) =>
    ['provenance', 'models', 'get', { id }] as const,
  provenanceRunsTrace: (id: string) =>
    ['provenance', 'runs', 'trace', { id }] as const,
  scenarioRunsGet: (id: string) =>
    ['scenario', 'runs', 'get', { id }] as const,
  scenarioRunsList: (id: string, limit?: number | undefined) =>
    ['scenario', 'runs', 'list', { id, limit }] as const,
  scenarioScenariosGet: (id: string) =>
    ['scenario', 'scenarios', 'get', { id }] as const,
  scenarioScenariosList: (limit?: number | undefined) =>
    ['scenario', 'scenarios', 'list', { limit }] as const,
  sclDevicesImpact: (id: string) =>
    ['scl', 'devices', 'impact', { id }] as const,
  sclDevicesList: (limit?: number | undefined) =>
    ['scl', 'devices', 'list', { limit }] as const,
  topologySubstationsGet: (id: string) =>
    ['topology', 'substations', 'get', { id }] as const,
  topologySubstationsList: (limit?: number | undefined) =>
    ['topology', 'substations', 'list', { limit }] as const,
  topologySubstationsReduction: (id: string, open?: string[] | undefined, closed?: string[] | undefined) =>
    ['topology', 'substations', 'reduction', { id, open, closed }] as const,
  weatherCyclesGet: (id: string) =>
    ['weather', 'cycles', 'get', { id }] as const,
  weatherCyclesGrids: (id: string) =>
    ['weather', 'cycles', 'grids', { id }] as const,
  weatherCyclesList: (model?: string | undefined, status?: string | undefined, limit?: number | undefined) =>
    ['weather', 'cycles', 'list', { model, status, limit }] as const,
  weatherSpansForecast: (id: string, field?: string | undefined, issued?: string, from?: string, to?: string) =>
    ['weather', 'spans', 'forecast', { id, field, issued, from, to }] as const,
  weatherStationsObservations: (id: string, field?: string | undefined, from?: string, to?: string) =>
    ['weather', 'stations', 'observations', { id, field, from, to }] as const,
} as const;

/** The corridor area within half_width_m (default 500, in meters on each side of a traced route) of every branch the run tripped. Corridors that overlap are returned whole rather than merged, so this is corridor area, not a single figure of distinct ground — see shape.Cover. */
export function useCascadeRunsExtent(
  client: RESTClient,
  id: string,
  halfWidthM?: number | undefined,
  options?: Omit<UseQueryOptions<types.extentResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.cascadeRunsExtent(id, halfWidthM),
    queryFn: ({ signal }) => client.cascade.runs.extent(id, halfWidthM, { signal }),
    ...options,
  });
}

/** One recorded simulation, with every event in the order it happened in simulated time. */
export function useCascadeRunsGet(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.CascadeRun, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.cascadeRunsGet(id),
    queryFn: ({ signal }) => client.cascade.runs.get(id, { signal }),
    ...options,
  });
}

/** The islanded state the network settled, truncated, or collapsed into, with each island's worst condition over the run and its final AC verdict. */
export function useCascadeRunsIslands(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.islandsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.cascadeRunsIslands(id),
    queryFn: ({ signal }) => client.cascade.runs.islands(id, { signal }),
    ...options,
  });
}

/** Every recorded cascade simulation of one network model, most recent first. */
export function useCascadeRunsList(
  client: RESTClient,
  modelHash?: string,
  limit?: number | undefined,
  options?: Omit<UseQueryOptions<types.cascadeRunsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.cascadeRunsList(modelHash, limit),
    queryFn: ({ signal }) => client.cascade.runs.list(modelHash, limit, { signal }),
    ...options,
  });
}

/** One recorded analysis, with every contingency in the order it was evaluated. That order is preserved because it is what was asked for; ranking is a separate view. */
export function useContingencyRunsGet(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.Run, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.contingencyRunsGet(id),
    queryFn: ({ signal }) => client.contingency.runs.get(id, { signal }),
    ...options,
  });
}

/** Every recorded N-1 analysis of one network model, most recent first. */
export function useContingencyRunsList(
  client: RESTClient,
  modelHash?: string,
  limit?: number | undefined,
  options?: Omit<UseQueryOptions<types.sweepRunsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.contingencyRunsList(modelHash, limit),
    queryFn: ({ signal }) => client.contingency.runs.list(modelHash, limit, { signal }),
    ...options,
  });
}

/** Worst first, with contingencies the screen could not evaluate ahead of everything else. A radial outage that islands part of the network is usually the most consequential thing on the list and is exactly what a linear factor cannot answer — ranking it by a loading it does not have would bury it. */
export function useContingencyRunsRanked(
  client: RESTClient,
  id: string,
  all?: boolean | undefined,
  options?: Omit<UseQueryOptions<types.rankedResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.contingencyRunsRanked(id, all),
    queryFn: ({ signal }) => client.contingency.runs.ranked(id, all, { signal }),
    ...options,
  });
}

export function useDatasourceFeedsGet(
  client: RESTClient,
  id: string,
  validAt?: string | undefined,
  knownAt?: string | undefined,
  options?: Omit<UseQueryOptions<types.Feed, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.datasourceFeedsGet(id, validAt, knownAt),
    queryFn: ({ signal }) => client.datasource.feeds.get(id, validAt, knownAt, { signal }),
    ...options,
  });
}

/** The retrievable resources within one data source. */
export function useDatasourceFeedsList(
  client: RESTClient,
  id: string,
  limit?: number | undefined,
  after?: string | undefined,
  validAt?: string | undefined,
  knownAt?: string | undefined,
  options?: Omit<UseQueryOptions<types.feedsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.datasourceFeedsList(id, limit, after, validAt, knownAt),
    queryFn: ({ signal }) => client.datasource.feeds.list(id, limit, after, validAt, knownAt, { signal }),
    ...options,
  });
}

/** Accepts either an identifier or a slug. A caller holding a slug from a document and one holding an identifier from an API response are asking the same question. */
export function useDatasourceSourcesGet(
  client: RESTClient,
  id: string,
  validAt?: string | undefined,
  knownAt?: string | undefined,
  options?: Omit<UseQueryOptions<types.Source, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.datasourceSourcesGet(id, validAt, knownAt),
    queryFn: ({ signal }) => client.datasource.sources.get(id, validAt, knownAt, { signal }),
    ...options,
  });
}

/** The public data source registry. Every dataset blackstart consumes traces to an entry here, and every provenance record points at one. */
export function useDatasourceSourcesList(
  client: RESTClient,
  kind?: string | undefined,
  slug?: string | undefined,
  limit?: number | undefined,
  after?: string | undefined,
  validAt?: string | undefined,
  knownAt?: string | undefined,
  options?: Omit<UseQueryOptions<types.sourcesResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.datasourceSourcesList(kind, slug, limit, after, validAt, knownAt),
    queryFn: ({ signal }) => client.datasource.sources.list(kind, slug, limit, after, validAt, knownAt, { signal }),
    ...options,
  });
}

export function useGeoBranchesRoute(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.Route, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.geoBranchesRoute(id),
    queryFn: ({ signal }) => client.geo.branches.route(id, { signal }),
    ...options,
  });
}

export function useGeoCircuitsGet(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.Circuit, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.geoCircuitsGet(id),
    queryFn: ({ signal }) => client.geo.circuits.get(id, { signal }),
    ...options,
  });
}

/** Transmission lines imported from public sources. These carry no electrical parameters and cannot appear in a power flow or a contingency; a route naming one is geometry only. */
export function useGeoCircuitsList(
  client: RESTClient,
  limit?: number | undefined,
  options?: Omit<UseQueryOptions<types.circuitsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.geoCircuitsList(limit),
    queryFn: ({ signal }) => client.geo.circuits.list(limit, { signal }),
    ...options,
  });
}

export function useGeoCircuitsRoute(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.Route, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.geoCircuitsRoute(id),
    queryFn: ({ signal }) => client.geo.circuits.route(id, { signal }),
    ...options,
  });
}

/** A branch's path over the ground, with the provenance of that path: surveyed means a source recorded where the structures actually are, interpolated means a straight great-circle line with evenly spaced imaginary ones. A per-span failure probability conditioned on the second is a different claim from one conditioned on the first. */
export function useGeoRoutesGet(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.Route, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.geoRoutesGet(id),
    queryFn: ({ signal }) => client.geo.routes.get(id, { signal }),
    ...options,
  });
}

export function useGeoRoutesList(
  client: RESTClient,
  limit?: number | undefined,
  options?: Omit<UseQueryOptions<types.routesResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.geoRoutesList(limit),
    queryFn: ({ signal }) => client.geo.routes.list(limit, { signal }),
    ...options,
  });
}

/** Geometry is per span because failure is per span: a conductor breaks between two structures, an ice load is local, and a 200 km line passes through several weather systems at once. */
export function useGeoRoutesSpans(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.spansResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.geoRoutesSpans(id),
    queryFn: ({ signal }) => client.geo.routes.spans(id, { signal }),
    ...options,
  });
}

/** Every assignment carries a basis. A surveyed assignment and one assumed from the voltage class are different claims, and a rating conditioned on the second is indicative rather than authoritative. */
export function useHazardAssignmentsList(
  client: RESTClient,
  limit?: number | undefined,
  after?: string | undefined,
  options?: Omit<UseQueryOptions<types.assignmentsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.hazardAssignmentsList(limit, after),
    queryFn: ({ signal }) => client.hazard.assignments.list(limit, after, { signal }),
    ...options,
  });
}

/** Each names the cycle it read and the provenance run it produced, and counts the spans it rated against the spans it skipped for want of a conductor assignment. */
export function useHazardComputationsList(
  client: RESTClient,
  limit?: number | undefined,
  after?: string | undefined,
  options?: Omit<UseQueryOptions<types.computationsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.hazardComputationsList(limit, after),
    queryFn: ({ signal }) => client.hazard.computations.list(limit, after, { signal }),
    ...options,
  });
}

/** Standard ACSR types with the properties IEEE 738 needs: diameter, resistance at two temperatures, emissivity, absorptivity and the maximum operating temperature a rating is the current at. */
export function useHazardConductorsList(
  client: RESTClient,
  options?: Omit<UseQueryOptions<types.conductorsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.hazardConductorsList(),
    queryFn: ({ signal }) => client.hazard.conductors.list({ signal }),
    ...options,
  });
}

/** Wind drag and ice weight in newtons per meter, ice thickness in meters, ice mass in kilograms per meter, and the resultant of drag and weight. Absent quantities are absent rather than zero: GFS publishes no freezing-rain accumulation, so a GFS-derived pass has no ice series at all. */
export function useHazardSpansLoading(
  client: RESTClient,
  id: string,
  issued?: string,
  from?: string,
  to?: string,
  options?: Omit<UseQueryOptions<types.valuesResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.hazardSpansLoading(id, issued, from, to),
    queryFn: ({ signal }) => client.hazard.spans.loading(id, issued, from, to, { signal }),
    ...options,
  });
}

/** Amperes, from IEEE 738's steady-state heat balance evaluated at the conductor's temperature limit. Issuance is a parameter rather than inferred, because "what is the rating now" and "what did the 06Z run say the rating would be" are different questions, and only the second can be checked afterwards. */
export function useHazardSpansRating(
  client: RESTClient,
  id: string,
  issued?: string,
  from?: string,
  to?: string,
  options?: Omit<UseQueryOptions<types.valuesResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.hazardSpansRating(id, issued, from, to),
    queryFn: ({ signal }) => client.hazard.spans.rating(id, issued, from, to, { signal }),
    ...options,
  });
}

/** The bytes the solver reads, exactly as stored. The content hash is the sha256 of these bytes, so anyone can verify that a result citing that hash was computed against this network and not another. */
export function useNetworkmodelEncoding(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.encodingResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.networkmodelEncoding(id),
    queryFn: ({ signal }) => client.networkmodel.encoding(id, { signal }),
    ...options,
  });
}

export function useNetworkmodelGet(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.NetworkModel, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.networkmodelGet(id),
    queryFn: ({ signal }) => client.networkmodel.get(id, { signal }),
    ...options,
  });
}

/** Every network this platform can be asked about, most recent first. A model is immutable: correcting one produces a new model with a new content hash, so a result citing the old hash keeps meaning what it meant. */
export function useNetworkmodelList(
  client: RESTClient,
  limit?: number | undefined,
  options?: Omit<UseQueryOptions<types.modelsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.networkmodelList(limit),
    queryFn: ({ signal }) => client.networkmodel.list(limit, { signal }),
    ...options,
  });
}

/** One solved power flow, with the solver version and model hash that produced it and whether reactive limits were enforced. */
export function usePowerflowSolutionsGet(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.Solution, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.powerflowSolutionsGet(id),
    queryFn: ({ signal }) => client.powerflow.solutions.get(id, { signal }),
    ...options,
  });
}

/** Every recorded solve of one network model, most recent first. The model is named by content hash, so two solves of the same network are found together whatever it was called. */
export function usePowerflowSolutionsList(
  client: RESTClient,
  modelHash?: string,
  limit?: number | undefined,
  options?: Omit<UseQueryOptions<types.solutionsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.powerflowSolutionsList(modelHash, limit),
    queryFn: ({ signal }) => client.powerflow.solutions.list(modelHash, limit, { signal }),
    ...options,
  });
}

/** Branch indices loaded above a threshold, worst first. Unrated branches are never reported: a zero rating means the limit is unknown, not zero. */
export function usePowerflowSolutionsViolations(
  client: RESTClient,
  id: string,
  threshold?: number | undefined,
  options?: Omit<UseQueryOptions<types.violationsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.powerflowSolutionsViolations(id, threshold),
    queryFn: ({ signal }) => client.powerflow.solutions.violations(id, threshold, { signal }),
    ...options,
  });
}

/** One retrieval of one external dataset: where it came from, the sha256 of what arrived, and the parser version that read it. */
export function useProvenanceDatasetsGet(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.DatasetVersion, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.provenanceDatasetsGet(id),
    queryFn: ({ signal }) => client.provenance.datasets.get(id, { signal }),
    ...options,
  });
}

/** A solver build, a parser, or a trained model, with the commit it was built from and the parameters that change its answer. */
export function useProvenanceModelsGet(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.ModelVersion, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.provenanceModelsGet(id),
    queryFn: ({ signal }) => client.provenance.models.get(id, { signal }),
    ...options,
  });
}

/** What produced this number: the run, the producer version and the commit it was built from, and every dataset version it read with the role each played. One call rather than a graph walk. */
export function useProvenanceRunsTrace(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.traceResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.provenanceRunsTrace(id),
    queryFn: ({ signal }) => client.provenance.runs.trace(id, { signal }),
    ...options,
  });
}

/** Freezes a copy of the scenario's manifest into a new run and returns it queued. A worker claims and executes it; poll the run for its status and, once complete, the sweep and cascade it produced. */
export function useScenarioRunsEnqueue(
  client: RESTClient,
  options?: Omit<UseMutationOptions<types.ScenarioRun, Error, { id: string }>, 'mutationFn'>,
) {
  return useMutation({
    mutationFn: ({ id }: { id: string }) => client.scenario.runs.enqueue(id),
    ...options,
  });
}

/** One run: the manifest it froze, where it is in its lifecycle, the step it failed at if it failed, and the sweep and cascade it produced if it completed. */
export function useScenarioRunsGet(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.ScenarioRun, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.scenarioRunsGet(id),
    queryFn: ({ signal }) => client.scenario.runs.get(id, { signal }),
    ...options,
  });
}

/** Every recorded run of one scenario, most recent first. */
export function useScenarioRunsList(
  client: RESTClient,
  id: string,
  limit?: number | undefined,
  options?: Omit<UseQueryOptions<types.scenarioRunsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.scenarioRunsList(id, limit),
    queryFn: ({ signal }) => client.scenario.runs.list(id, limit, { signal }),
    ...options,
  });
}

/** Records the binding a run will execute: a network model by content hash, a forecast cycle and the moment within it, a contingency policy and a seed. Naming an existing scenario revises it, which changes what the next run executes and nothing about any run already recorded. */
export function useScenarioScenariosDefine(
  client: RESTClient,
  options?: Omit<UseMutationOptions<types.Scenario, Error, { body: Record<string, any> }>, 'mutationFn'>,
) {
  return useMutation({
    mutationFn: ({ body }: { body: Record<string, any> }) => client.scenario.scenarios.define(body),
    ...options,
  });
}

/** One scenario and the manifest it currently binds. */
export function useScenarioScenariosGet(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.Scenario, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.scenarioScenariosGet(id),
    queryFn: ({ signal }) => client.scenario.scenarios.get(id, { signal }),
    ...options,
  });
}

/** Defined scenarios, most recently created first. */
export function useScenarioScenariosList(
  client: RESTClient,
  limit?: number | undefined,
  options?: Omit<UseQueryOptions<types.scenariosResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.scenarioScenariosList(limit),
    queryFn: ({ signal }) => client.scenario.scenarios.list(limit, { signal }),
    ...options,
  });
}

/** The communication graph around one device: who subscribes to it, what it subscribes to, and what it publishes. The first is the question the electrical model cannot answer at all — two relays on the same bus may exchange nothing, and two relays in different substations may be tightly coupled.  Advisory. Nothing here commands anything; it reports what a configuration file says. */
export function useSclDevicesImpact(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.impactResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.sclDevicesImpact(id),
    queryFn: ({ signal }) => client.scl.devices.impact(id, { signal }),
    ...options,
  });
}

/** Every intelligent electronic device an imported configuration describes, by the name every external reference resolves against. */
export function useSclDevicesList(
  client: RESTClient,
  limit?: number | undefined,
  options?: Omit<UseQueryOptions<types.devicesResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.sclDevicesList(limit),
    queryFn: ({ signal }) => client.scl.devices.list(limit, { signal }),
    ...options,
  });
}

export function useTopologySubstationsGet(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.Substation, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.topologySubstationsGet(id),
    queryFn: ({ signal }) => client.topology.substations.get(id, { signal }),
    ...options,
  });
}

/** Every site in the network model. */
export function useTopologySubstationsList(
  client: RESTClient,
  limit?: number | undefined,
  options?: Omit<UseQueryOptions<types.substationsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.topologySubstationsList(limit),
    queryFn: ({ signal }) => client.topology.substations.list(limit, { signal }),
    ...options,
  });
}

/** Collapses every set of connectivity nodes joined by closed zero-impedance devices into one bus. Switch positions come from each device's recorded normal state; open and closed override individual devices, which is how a study asks what would happen if one breaker moved. Derived on every call and never stored — two models of one network drift, and the drift stays invisible until a result disagrees with a diagram. */
export function useTopologySubstationsReduction(
  client: RESTClient,
  id: string,
  open?: string[] | undefined,
  closed?: string[] | undefined,
  options?: Omit<UseQueryOptions<types.reductionResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.topologySubstationsReduction(id, open, closed),
    queryFn: ({ signal }) => client.topology.substations.reduction(id, open, closed, { signal }),
    ...options,
  });
}

export function useWeatherCyclesGet(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.Cycle, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.weatherCyclesGet(id),
    queryFn: ({ signal }) => client.weather.cycles.get(id, { signal }),
    ...options,
  });
}

/** One entry per field per forecast hour, each naming the bytes it was decoded from and the dataset version that recorded the retrieval. Covered and total say how many spans fell inside the grid's domain — HRRR is CONUS-only, so a span outside it has no sample rather than a zero. */
export function useWeatherCyclesGrids(
  client: RESTClient,
  id: string,
  options?: Omit<UseQueryOptions<types.gridsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.weatherCyclesGrids(id),
    queryFn: ({ signal }) => client.weather.cycles.grids(id, { signal }),
    ...options,
  });
}

export function useWeatherCyclesList(
  client: RESTClient,
  model?: string | undefined,
  status?: string | undefined,
  limit?: number | undefined,
  options?: Omit<UseQueryOptions<types.cyclesResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.weatherCyclesList(model, status, limit),
    queryFn: ({ signal }) => client.weather.cycles.list(model, status, limit, { signal }),
    ...options,
  });
}

/** Issuance is a parameter rather than inferred from the window, because "what does the latest run say" and "what did the 06Z run say" are different questions — and a reader who cannot ask the second cannot check a forecast against what happened. */
export function useWeatherSpansForecast(
  client: RESTClient,
  id: string,
  field?: string | undefined,
  issued?: string,
  from?: string,
  to?: string,
  options?: Omit<UseQueryOptions<types.forecastResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.weatherSpansForecast(id, field, issued, from, to),
    queryFn: ({ signal }) => client.weather.spans.forecast(id, field, issued, from, to, { signal }),
    ...options,
  });
}

export function useWeatherStationsObservations(
  client: RESTClient,
  id: string,
  field?: string | undefined,
  from?: string,
  to?: string,
  options?: Omit<UseQueryOptions<types.observationsResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: queryKeys.weatherStationsObservations(id, field, from, to),
    queryFn: ({ signal }) => client.weather.stations.observations(id, field, from, to, { signal }),
    ...options,
  });
}

