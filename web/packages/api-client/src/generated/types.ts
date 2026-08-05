// Generated types

export enum ConnectionState {
  DISCONNECTED = 'disconnected',
  CONNECTING = 'connecting',
  CONNECTED = 'connected',
  RECONNECTING = 'reconnecting',
  CLOSED = 'closed',
  ERROR = 'error',
}

export interface Assignment {
  basis: string;
  branchId: string;
  conductorCode: string;
  id: string;
  note?: string;
}

export interface BlackstartRun {
  elapsedS: number;
  events: Event[];
  finishedAt: string;
  id: string;
  initiating: string[];
  islands: Island[];
  loadLost: number;
  modelHash: string;
  outcome: string;
  solverVersion: string;
  startedAt: string;
  steps: number;
}

export interface Branch {
  equipmentId: string;
  from: number;
  kind: string;
  to: number;
}

export interface Bus {
  nodes: string[];
  number: number;
  voltageLevelId: string;
}

export interface Candidate {
  branch: number;
  contingencyId: string;
  screenability: string;
  violations: Violation[];
  worstLoading?: number;
}

export interface Computation {
  computedAt: string;
  cycleId: string;
  id: string;
  runId: string;
  spans: number;
  unrated: number;
  values: number;
}

export interface Conductor {
  absorptivity: number;
  areaMm2: number;
  code: string;
  diameterM: number;
  emissivity: number;
  highTempC: number;
  lowTempC: number;
  maxTempC: number;
  name: string;
  resistanceHighOhmPerM: number;
  resistanceLowOhmPerM: number;
}

export interface Cycle {
  feedId: string;
  id: string;
  issuedAt: string;
  model: string;
  status: string;
}

export interface DatasetVersion {
  contentHash: string;
  id: string;
  parserVersion: string;
  retrievedAt: string;
  sizeBytes: number;
  sourceId: string;
  url: string;
}

export interface Event {
  at: number;
  detail: string;
  element: string;
  island?: number;
  kind: string;
}

export interface Feed {
  cadence: string;
  format: string;
  id: string;
  name: string;
  slug: string;
  sourceId: string;
  url: string;
}

export interface Grid {
  blobRef: string;
  covered: number;
  cycleId: string;
  datasetVersionId: string;
  field: string;
  forecastHour: number;
  id: string;
  nx: number;
  ny: number;
  projection: string;
  sampledAt: string;
  total: number;
}

export interface IED {
  configVersion: string;
  description: string;
  id: string;
  manufacturer: string;
  name: string;
  type: string;
}

export interface Injection {
  bus: number;
  equipmentId: string;
  kind: string;
}

export interface Island {
  balance: number;
  busCount: number;
  buses: number[];
  condition: string;
  frequency?: number;
  nadir?: number;
  rocof?: number;
  shed: number;
  trace?: RunSample[];
  verdict: string;
}

export interface ModelVersion {
  codeCommit: string;
  id: string;
  kind: string;
  name: string;
  parameters: Record<string, any>;
  recordedAt: string;
  version: string;
}

export interface NetworkModel {
  blobRef: string;
  branchCount: number;
  builtAt: string;
  busCount: number;
  contentHash: string;
  datasetVersionId: string;
  id: string;
  name: string;
  sourceKind: string;
}

export interface Observation {
  field: string;
  observedAt: string;
  stationId: string;
  value: number;
}

export interface Point {
  elevation: number;
  lat: number;
  lon: number;
}

export interface Publication {
  appId: string;
  cbName: string;
  confRev: number;
  dataSet: string;
  id: string;
  iedId: string;
  iedName: string;
  ldInst: string;
  mac: string;
  networkAppId: string;
  vlan: string;
}

export interface Reduction {
  branches: Branch[];
  buses: Bus[];
  injections: Injection[];
  islands: number[][];
}

export interface Route {
  branchId: string;
  id: string;
  lengthM: number;
  provenance: string;
}

export interface Run {
  elapsedS: number;
  events: Event[];
  finishedAt: string;
  id: string;
  initiating: string[];
  islands: Island[];
  loadLost: number;
  modelHash: string;
  outcome: string;
  solverVersion: string;
  startedAt: string;
  steps: number;
}

export interface RunSample {
  at: number;
  hz: number;
}

export interface Sample {
  field: string;
  issuedAt: string;
  spanId: string;
  validAt: string;
  value: number;
}

export interface Solution {
  bindingGenerators: number[];
  branches: SolutionBranch[];
  buses: SolutionBus[];
  enforcedReactiveLimits: boolean;
  id: string;
  iterations: number;
  method: string;
  modelHash: string;
  residual: number;
  solvedAt: string;
  solverVersion: string;
}

export interface SolutionBranch {
  loading: number;
  pFlow: number;
}

export interface SolutionBus {
  pInjection: number;
  qInjection: number;
  vAngle: number;
  vMagnitude: number;
}

export interface Source {
  homepage: string;
  id: string;
  kind: string;
  license: string;
  name: string;
  provides: string;
  requiresAttribution: boolean;
  slug: string;
}

export interface Span {
  azimuthDeg: number;
  geometry: Point[];
  id: string;
  lengthM: number;
  routeId: string;
  sequence: number;
}

export interface SpanValue {
  issuedAt: string;
  quantity: string;
  spanId: string;
  validAt: string;
  value: number;
}

export interface Subscription {
  data: string;
  id: string;
  publisherCb: string;
  publisherId: string;
  relation: string;
  resolved: boolean;
  subscriberId: string;
  subscriberPath: string;
}

export interface Substation {
  code: string;
  id: string;
  name: string;
}

export interface SweepRun {
  candidates: Candidate[];
  finishedAt: string;
  id: string;
  modelHash: string;
  solverVersion: string;
  startedAt: string;
}

export interface TracedInput {
  dataset: DatasetVersion;
  role: string;
}

export interface Violation {
  branch: number;
  loading: number;
  pFlow: number;
  rating: number;
}

export interface assignmentsResponse {
  assignments: Assignment[];
}

export interface cascadeRunsResponse {
  runs: BlackstartRun[];
}

export interface computationsResponse {
  computations: Computation[];
}

export interface conductorsResponse {
  conductors: Conductor[];
}

export interface cyclesResponse {
  cycles: Cycle[];
}

export interface devicesResponse {
  devices: IED[];
}

export interface encodingResponse {
  contentHash: string;
  encoding: number[];
  modelId: string;
  sizeBytes: number;
}

export interface feedsResponse {
  feeds: Feed[];
}

export interface forecastResponse {
  issuedAt: string;
  samples: Sample[];
  spanId: string;
}

export interface gridsResponse {
  cycleId: string;
  grids: Grid[];
}

export interface impactResponse {
  device: IED;
  listeners: Subscription[];
  listensTo: Subscription[];
  publications: Publication[];
  unaddressed: number;
}

export interface islandsResponse {
  islands: Island[];
  runId: string;
}

export interface modelsResponse {
  models: NetworkModel[];
}

export interface observationsResponse {
  readings: Observation[];
  stationId: string;
}

export interface rankedResponse {
  candidates: Candidate[];
  runId: string;
  unevaluated: number;
}

export interface reductionResponse {
  overrides?: Record<string, any>;
  reduction: Reduction;
  substationId: string;
}

export interface routesResponse {
  routes: Route[];
}

export interface solutionsResponse {
  solutions: Solution[];
}

export interface sourcesResponse {
  sources: Source[];
}

export interface spansResponse {
  lengthM: number;
  routeId: string;
  spans: Span[];
}

export interface substationsResponse {
  substations: Substation[];
}

export interface sweepRunsResponse {
  runs: SweepRun[];
}

export interface traceResponse {
  inputs: TracedInput[];
  producer: ModelVersion;
  reproducible: boolean;
  run: Run;
}

export interface valuesResponse {
  issuedAt: string;
  spanId: string;
  values: SpanValue[];
}

export interface violationsResponse {
  branches: number[];
  solutionId: string;
  threshold: number;
}

export interface AuthConfig {
  bearerToken?: string;
  apiKey?: string;
  customHeaders?: Record<string, string>;
}

export interface ClientConfig {
  baseURL: string;
  auth?: AuthConfig;
  timeout?: number;
}

