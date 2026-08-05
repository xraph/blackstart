// Generated codec table
//
// Describes, per schema, how a wire payload maps onto its TypeScript
// shape. `ts` is the client-side field name derived from the wire name by
// the configured FieldNaming strategy (or a FieldOverrides entry); encode
// and decode below walk this table to rename between the two.

export type Codec =
  | { kind: 'object'; fields: Record<string, { ts: string; codec?: string | undefined }>; required?: string[]; values?: string }
  | { kind: 'array'; items?: string }
  | { kind: 'record'; values?: string }
  | { kind: 'union'; discriminator?: { wire: string; map: Record<string, string> }; members: string[] }
  | { kind: 'passthrough' };

export const CODECS: Record<string, Codec> = {
  "Assignment": {"kind": "object", "fields": {"basis": {"ts": "basis"}, "branch_id": {"ts": "branchId"}, "conductor_code": {"ts": "conductorCode"}, "id": {"ts": "id"}, "note": {"ts": "note"}}, "required": ["basis", "branch_id", "conductor_code", "id"]},
  "Branch": {"kind": "object", "fields": {"EquipmentID": {"ts": "equipmentId"}, "From": {"ts": "from"}, "Kind": {"ts": "kind"}, "To": {"ts": "to"}}, "required": ["EquipmentID", "From", "Kind", "To"]},
  "Bus": {"kind": "object", "fields": {"Nodes": {"ts": "nodes", "codec": "Bus.Nodes"}, "Number": {"ts": "number"}, "VoltageLevelID": {"ts": "voltageLevelId"}}, "required": ["Nodes", "Number", "VoltageLevelID"]},
  "Bus.Nodes": {"kind": "array"},
  "Candidate": {"kind": "object", "fields": {"branch": {"ts": "branch"}, "contingency_id": {"ts": "contingencyId"}, "screenability": {"ts": "screenability"}, "violations": {"ts": "violations", "codec": "Candidate.violations"}, "worst_loading": {"ts": "worstLoading"}}, "required": ["branch", "contingency_id", "screenability", "violations"]},
  "Candidate.violations": {"kind": "array", "items": "Violation"},
  "CascadeRun": {"kind": "object", "fields": {"elapsed_s": {"ts": "elapsedS"}, "events": {"ts": "events", "codec": "CascadeRun.events"}, "finished_at": {"ts": "finishedAt"}, "id": {"ts": "id"}, "initiating": {"ts": "initiating", "codec": "CascadeRun.initiating"}, "islands": {"ts": "islands", "codec": "CascadeRun.islands"}, "load_lost": {"ts": "loadLost"}, "model_hash": {"ts": "modelHash"}, "outcome": {"ts": "outcome"}, "s_base_va": {"ts": "sBaseVa"}, "solver_version": {"ts": "solverVersion"}, "started_at": {"ts": "startedAt"}, "steps": {"ts": "steps"}}, "required": ["elapsed_s", "events", "finished_at", "id", "initiating", "islands", "load_lost", "model_hash", "outcome", "s_base_va", "solver_version", "started_at", "steps"]},
  "CascadeRun.events": {"kind": "array", "items": "Event"},
  "CascadeRun.initiating": {"kind": "array"},
  "CascadeRun.islands": {"kind": "array", "items": "Island"},
  "Circuit": {"kind": "object", "fields": {"external_id": {"ts": "externalId"}, "id": {"ts": "id"}, "operator": {"ts": "operator"}, "source": {"ts": "source"}, "voltage_kv": {"ts": "voltageKv"}}, "required": ["external_id", "id", "operator", "source", "voltage_kv"]},
  "Computation": {"kind": "object", "fields": {"computed_at": {"ts": "computedAt"}, "cycle_id": {"ts": "cycleId"}, "id": {"ts": "id"}, "run_id": {"ts": "runId"}, "spans": {"ts": "spans"}, "unrated": {"ts": "unrated"}, "values": {"ts": "values"}}, "required": ["computed_at", "cycle_id", "id", "run_id", "spans", "unrated", "values"]},
  "Conductor": {"kind": "object", "fields": {"absorptivity": {"ts": "absorptivity"}, "area_mm2": {"ts": "areaMm2"}, "code": {"ts": "code"}, "diameter_m": {"ts": "diameterM"}, "emissivity": {"ts": "emissivity"}, "high_temp_c": {"ts": "highTempC"}, "low_temp_c": {"ts": "lowTempC"}, "max_temp_c": {"ts": "maxTempC"}, "name": {"ts": "name"}, "resistance_high_ohm_per_m": {"ts": "resistanceHighOhmPerM"}, "resistance_low_ohm_per_m": {"ts": "resistanceLowOhmPerM"}}, "required": ["absorptivity", "area_mm2", "code", "diameter_m", "emissivity", "high_temp_c", "low_temp_c", "max_temp_c", "name", "resistance_high_ohm_per_m", "resistance_low_ohm_per_m"]},
  "Cycle": {"kind": "object", "fields": {"feed_id": {"ts": "feedId"}, "id": {"ts": "id"}, "issued_at": {"ts": "issuedAt"}, "model": {"ts": "model"}, "status": {"ts": "status"}}, "required": ["feed_id", "id", "issued_at", "model", "status"]},
  "DatasetVersion": {"kind": "object", "fields": {"ContentHash": {"ts": "contentHash"}, "ID": {"ts": "id"}, "ParserVersion": {"ts": "parserVersion"}, "RetrievedAt": {"ts": "retrievedAt"}, "SizeBytes": {"ts": "sizeBytes"}, "SourceID": {"ts": "sourceId"}, "URL": {"ts": "url"}}, "required": ["ContentHash", "ID", "ParserVersion", "RetrievedAt", "SizeBytes", "SourceID", "URL"]},
  "Event": {"kind": "object", "fields": {"at": {"ts": "at"}, "detail": {"ts": "detail"}, "element": {"ts": "element"}, "island": {"ts": "island"}, "kind": {"ts": "kind"}}, "required": ["at", "detail", "element", "kind"]},
  "Feed": {"kind": "object", "fields": {"cadence": {"ts": "cadence"}, "format": {"ts": "format"}, "id": {"ts": "id"}, "name": {"ts": "name"}, "slug": {"ts": "slug"}, "sourceId": {"ts": "sourceId"}, "url": {"ts": "url"}}, "required": ["cadence", "format", "id", "name", "slug", "sourceId", "url"]},
  "Grid": {"kind": "object", "fields": {"blob_ref": {"ts": "blobRef"}, "covered": {"ts": "covered"}, "cycle_id": {"ts": "cycleId"}, "dataset_version_id": {"ts": "datasetVersionId"}, "field": {"ts": "field"}, "forecast_hour": {"ts": "forecastHour"}, "id": {"ts": "id"}, "nx": {"ts": "nx"}, "ny": {"ts": "ny"}, "projection": {"ts": "projection"}, "sampled_at": {"ts": "sampledAt"}, "total": {"ts": "total"}}, "required": ["blob_ref", "covered", "cycle_id", "dataset_version_id", "field", "forecast_hour", "id", "nx", "ny", "projection", "sampled_at", "total"]},
  "IED": {"kind": "object", "fields": {"config_version": {"ts": "configVersion"}, "description": {"ts": "description"}, "id": {"ts": "id"}, "manufacturer": {"ts": "manufacturer"}, "name": {"ts": "name"}, "type": {"ts": "type"}}, "required": ["config_version", "description", "id", "manufacturer", "name", "type"]},
  "Injection": {"kind": "object", "fields": {"Bus": {"ts": "bus"}, "EquipmentID": {"ts": "equipmentId"}, "Kind": {"ts": "kind"}}, "required": ["Bus", "EquipmentID", "Kind"]},
  "Island": {"kind": "object", "fields": {"balance": {"ts": "balance"}, "bus_count": {"ts": "busCount"}, "buses": {"ts": "buses", "codec": "Island.buses"}, "condition": {"ts": "condition"}, "frequency": {"ts": "frequency"}, "nadir": {"ts": "nadir"}, "rocof": {"ts": "rocof"}, "shed": {"ts": "shed"}, "trace": {"ts": "trace", "codec": "Island.trace"}, "verdict": {"ts": "verdict"}}, "required": ["balance", "bus_count", "buses", "condition", "shed", "verdict"]},
  "Island.buses": {"kind": "array"},
  "Island.trace": {"kind": "array", "items": "RunSample"},
  "ModelVersion": {"kind": "object", "fields": {"CodeCommit": {"ts": "codeCommit"}, "ID": {"ts": "id"}, "Kind": {"ts": "kind"}, "Name": {"ts": "name"}, "Parameters": {"ts": "parameters", "codec": "ModelVersion.Parameters"}, "RecordedAt": {"ts": "recordedAt"}, "Version": {"ts": "version"}}, "required": ["CodeCommit", "ID", "Kind", "Name", "Parameters", "RecordedAt", "Version"]},
  "ModelVersion.Parameters": {"kind": "record"},
  "NetworkModel": {"kind": "object", "fields": {"blob_ref": {"ts": "blobRef"}, "branch_count": {"ts": "branchCount"}, "built_at": {"ts": "builtAt"}, "bus_count": {"ts": "busCount"}, "content_hash": {"ts": "contentHash"}, "dataset_version_id": {"ts": "datasetVersionId"}, "id": {"ts": "id"}, "name": {"ts": "name"}, "source_kind": {"ts": "sourceKind"}}, "required": ["blob_ref", "branch_count", "built_at", "bus_count", "content_hash", "dataset_version_id", "id", "name", "source_kind"]},
  "Observation": {"kind": "object", "fields": {"field": {"ts": "field"}, "observed_at": {"ts": "observedAt"}, "station_id": {"ts": "stationId"}, "value": {"ts": "value"}}, "required": ["field", "observed_at", "station_id", "value"]},
  "Point": {"kind": "object", "fields": {"elevation": {"ts": "elevation"}, "lat": {"ts": "lat"}, "lon": {"ts": "lon"}}, "required": ["elevation", "lat", "lon"]},
  "ProvenanceRun": {"kind": "object", "fields": {"FinishedAt": {"ts": "finishedAt"}, "ID": {"ts": "id"}, "Kind": {"ts": "kind"}, "ModelVersionID": {"ts": "modelVersionId"}, "ResultHash": {"ts": "resultHash"}, "Seed": {"ts": "seed"}, "StartedAt": {"ts": "startedAt"}}, "required": ["FinishedAt", "ID", "Kind", "ModelVersionID", "ResultHash", "Seed", "StartedAt"]},
  "Publication": {"kind": "object", "fields": {"app_id": {"ts": "appId"}, "cb_name": {"ts": "cbName"}, "conf_rev": {"ts": "confRev"}, "data_set": {"ts": "dataSet"}, "id": {"ts": "id"}, "ied_id": {"ts": "iedId"}, "ied_name": {"ts": "iedName"}, "ld_inst": {"ts": "ldInst"}, "mac": {"ts": "mac"}, "network_app_id": {"ts": "networkAppId"}, "vlan": {"ts": "vlan"}}, "required": ["app_id", "cb_name", "conf_rev", "data_set", "id", "ied_id", "ied_name", "ld_inst", "mac", "network_app_id", "vlan"]},
  "Reduction": {"kind": "object", "fields": {"Branches": {"ts": "branches", "codec": "Reduction.Branches"}, "Buses": {"ts": "buses", "codec": "Reduction.Buses"}, "Injections": {"ts": "injections", "codec": "Reduction.Injections"}, "Islands": {"ts": "islands", "codec": "Reduction.Islands"}}, "required": ["Branches", "Buses", "Injections", "Islands"]},
  "Reduction.Branches": {"kind": "array", "items": "Branch"},
  "Reduction.Buses": {"kind": "array", "items": "Bus"},
  "Reduction.Injections": {"kind": "array", "items": "Injection"},
  "Reduction.Islands": {"kind": "array", "items": "Reduction.Islands.items"},
  "Reduction.Islands.items": {"kind": "array"},
  "Route": {"kind": "object", "fields": {"carrier_id": {"ts": "carrierId"}, "id": {"ts": "id"}, "length_m": {"ts": "lengthM"}, "provenance": {"ts": "provenance"}}, "required": ["carrier_id", "id", "length_m", "provenance"]},
  "Run": {"kind": "object", "fields": {"Candidates": {"ts": "candidates", "codec": "Run.Candidates"}, "FinishedAt": {"ts": "finishedAt"}, "ID": {"ts": "id"}, "ModelHash": {"ts": "modelHash"}, "SolverVersion": {"ts": "solverVersion"}, "StartedAt": {"ts": "startedAt"}}, "required": ["Candidates", "FinishedAt", "ID", "ModelHash", "SolverVersion", "StartedAt"]},
  "Run.Candidates": {"kind": "array", "items": "Candidate"},
  "RunSample": {"kind": "object", "fields": {"at": {"ts": "at"}, "hz": {"ts": "hz"}}, "required": ["at", "hz"]},
  "Sample": {"kind": "object", "fields": {"field": {"ts": "field"}, "issued_at": {"ts": "issuedAt"}, "span_id": {"ts": "spanId"}, "valid_at": {"ts": "validAt"}, "value": {"ts": "value"}}, "required": ["field", "issued_at", "span_id", "valid_at", "value"]},
  "Solution": {"kind": "object", "fields": {"BindingGenerators": {"ts": "bindingGenerators", "codec": "Solution.BindingGenerators"}, "Branches": {"ts": "branches", "codec": "Solution.Branches"}, "Buses": {"ts": "buses", "codec": "Solution.Buses"}, "EnforcedReactiveLimits": {"ts": "enforcedReactiveLimits"}, "ID": {"ts": "id"}, "Iterations": {"ts": "iterations"}, "Method": {"ts": "method"}, "ModelHash": {"ts": "modelHash"}, "Residual": {"ts": "residual"}, "SolvedAt": {"ts": "solvedAt"}, "SolverVersion": {"ts": "solverVersion"}}, "required": ["BindingGenerators", "Branches", "Buses", "EnforcedReactiveLimits", "ID", "Iterations", "Method", "ModelHash", "Residual", "SolvedAt", "SolverVersion"]},
  "Solution.BindingGenerators": {"kind": "array"},
  "Solution.Branches": {"kind": "array", "items": "SolutionBranch"},
  "Solution.Buses": {"kind": "array", "items": "SolutionBus"},
  "SolutionBranch": {"kind": "object", "fields": {"loading": {"ts": "loading"}, "p_flow": {"ts": "pFlow"}}, "required": ["loading", "p_flow"]},
  "SolutionBus": {"kind": "object", "fields": {"p_injection": {"ts": "pInjection"}, "q_injection": {"ts": "qInjection"}, "v_angle": {"ts": "vAngle"}, "v_magnitude": {"ts": "vMagnitude"}}, "required": ["p_injection", "q_injection", "v_angle", "v_magnitude"]},
  "Source": {"kind": "object", "fields": {"homepage": {"ts": "homepage"}, "id": {"ts": "id"}, "kind": {"ts": "kind"}, "license": {"ts": "license"}, "name": {"ts": "name"}, "provides": {"ts": "provides"}, "requiresAttribution": {"ts": "requiresAttribution"}, "slug": {"ts": "slug"}}, "required": ["homepage", "id", "kind", "license", "name", "provides", "requiresAttribution", "slug"]},
  "Span": {"kind": "object", "fields": {"azimuth_deg": {"ts": "azimuthDeg"}, "geometry": {"ts": "geometry", "codec": "Span.geometry"}, "id": {"ts": "id"}, "length_m": {"ts": "lengthM"}, "route_id": {"ts": "routeId"}, "sequence": {"ts": "sequence"}}, "required": ["azimuth_deg", "geometry", "id", "length_m", "route_id", "sequence"]},
  "Span.geometry": {"kind": "array", "items": "Point"},
  "SpanValue": {"kind": "object", "fields": {"issued_at": {"ts": "issuedAt"}, "quantity": {"ts": "quantity"}, "span_id": {"ts": "spanId"}, "valid_at": {"ts": "validAt"}, "value": {"ts": "value"}}, "required": ["issued_at", "quantity", "span_id", "valid_at", "value"]},
  "Subscription": {"kind": "object", "fields": {"data": {"ts": "data"}, "id": {"ts": "id"}, "publisher_cb": {"ts": "publisherCb"}, "publisher_id": {"ts": "publisherId"}, "relation": {"ts": "relation"}, "resolved": {"ts": "resolved"}, "subscriber_id": {"ts": "subscriberId"}, "subscriber_path": {"ts": "subscriberPath"}}, "required": ["data", "id", "publisher_cb", "publisher_id", "relation", "resolved", "subscriber_id", "subscriber_path"]},
  "Substation": {"kind": "object", "fields": {"Code": {"ts": "code"}, "ID": {"ts": "id"}, "Name": {"ts": "name"}}, "required": ["Code", "ID", "Name"]},
  "TracedInput": {"kind": "object", "fields": {"dataset": {"ts": "dataset", "codec": "DatasetVersion"}, "role": {"ts": "role"}}, "required": ["dataset", "role"]},
  "Violation": {"kind": "object", "fields": {"branch": {"ts": "branch"}, "loading": {"ts": "loading"}, "p_flow": {"ts": "pFlow"}, "rating": {"ts": "rating"}}, "required": ["branch", "loading", "p_flow", "rating"]},
  "assignmentsResponse": {"kind": "object", "fields": {"assignments": {"ts": "assignments", "codec": "assignmentsResponse.assignments"}}, "required": ["assignments"]},
  "assignmentsResponse.assignments": {"kind": "array", "items": "Assignment"},
  "cascadeRunsResponse": {"kind": "object", "fields": {"runs": {"ts": "runs", "codec": "cascadeRunsResponse.runs"}}, "required": ["runs"]},
  "cascadeRunsResponse.runs": {"kind": "array", "items": "CascadeRun"},
  "circuitsResponse": {"kind": "object", "fields": {"circuits": {"ts": "circuits", "codec": "circuitsResponse.circuits"}}, "required": ["circuits"]},
  "circuitsResponse.circuits": {"kind": "array", "items": "Circuit"},
  "computationsResponse": {"kind": "object", "fields": {"computations": {"ts": "computations", "codec": "computationsResponse.computations"}}, "required": ["computations"]},
  "computationsResponse.computations": {"kind": "array", "items": "Computation"},
  "conductorsResponse": {"kind": "object", "fields": {"conductors": {"ts": "conductors", "codec": "conductorsResponse.conductors"}}, "required": ["conductors"]},
  "conductorsResponse.conductors": {"kind": "array", "items": "Conductor"},
  "cyclesResponse": {"kind": "object", "fields": {"cycles": {"ts": "cycles", "codec": "cyclesResponse.cycles"}}, "required": ["cycles"]},
  "cyclesResponse.cycles": {"kind": "array", "items": "Cycle"},
  "devicesResponse": {"kind": "object", "fields": {"devices": {"ts": "devices", "codec": "devicesResponse.devices"}}, "required": ["devices"]},
  "devicesResponse.devices": {"kind": "array", "items": "IED"},
  "encodingResponse": {"kind": "object", "fields": {"content_hash": {"ts": "contentHash"}, "encoding": {"ts": "encoding", "codec": "encodingResponse.encoding"}, "model_id": {"ts": "modelId"}, "size_bytes": {"ts": "sizeBytes"}}, "required": ["content_hash", "encoding", "model_id", "size_bytes"]},
  "encodingResponse.encoding": {"kind": "array"},
  "feedsResponse": {"kind": "object", "fields": {"feeds": {"ts": "feeds", "codec": "feedsResponse.feeds"}}, "required": ["feeds"]},
  "feedsResponse.feeds": {"kind": "array", "items": "Feed"},
  "forecastResponse": {"kind": "object", "fields": {"issued_at": {"ts": "issuedAt"}, "samples": {"ts": "samples", "codec": "forecastResponse.samples"}, "span_id": {"ts": "spanId"}}, "required": ["issued_at", "samples", "span_id"]},
  "forecastResponse.samples": {"kind": "array", "items": "Sample"},
  "gridsResponse": {"kind": "object", "fields": {"cycle_id": {"ts": "cycleId"}, "grids": {"ts": "grids", "codec": "gridsResponse.grids"}}, "required": ["cycle_id", "grids"]},
  "gridsResponse.grids": {"kind": "array", "items": "Grid"},
  "impactResponse": {"kind": "object", "fields": {"device": {"ts": "device", "codec": "IED"}, "listeners": {"ts": "listeners", "codec": "impactResponse.listeners"}, "listens_to": {"ts": "listensTo", "codec": "impactResponse.listens_to"}, "publications": {"ts": "publications", "codec": "impactResponse.publications"}, "unaddressed": {"ts": "unaddressed"}}, "required": ["device", "listeners", "listens_to", "publications", "unaddressed"]},
  "impactResponse.listeners": {"kind": "array", "items": "Subscription"},
  "impactResponse.listens_to": {"kind": "array", "items": "Subscription"},
  "impactResponse.publications": {"kind": "array", "items": "Publication"},
  "islandsResponse": {"kind": "object", "fields": {"islands": {"ts": "islands", "codec": "islandsResponse.islands"}, "run_id": {"ts": "runId"}}, "required": ["islands", "run_id"]},
  "islandsResponse.islands": {"kind": "array", "items": "Island"},
  "modelsResponse": {"kind": "object", "fields": {"models": {"ts": "models", "codec": "modelsResponse.models"}}, "required": ["models"]},
  "modelsResponse.models": {"kind": "array", "items": "NetworkModel"},
  "observationsResponse": {"kind": "object", "fields": {"readings": {"ts": "readings", "codec": "observationsResponse.readings"}, "station_id": {"ts": "stationId"}}, "required": ["readings", "station_id"]},
  "observationsResponse.readings": {"kind": "array", "items": "Observation"},
  "rankedResponse": {"kind": "object", "fields": {"candidates": {"ts": "candidates", "codec": "rankedResponse.candidates"}, "run_id": {"ts": "runId"}, "unevaluated": {"ts": "unevaluated"}}, "required": ["candidates", "run_id", "unevaluated"]},
  "rankedResponse.candidates": {"kind": "array", "items": "Candidate"},
  "reductionResponse": {"kind": "object", "fields": {"overrides": {"ts": "overrides", "codec": "reductionResponse.overrides"}, "reduction": {"ts": "reduction", "codec": "Reduction"}, "substation_id": {"ts": "substationId"}}, "required": ["reduction", "substation_id"]},
  "reductionResponse.overrides": {"kind": "record"},
  "routesResponse": {"kind": "object", "fields": {"routes": {"ts": "routes", "codec": "routesResponse.routes"}}, "required": ["routes"]},
  "routesResponse.routes": {"kind": "array", "items": "Route"},
  "solutionsResponse": {"kind": "object", "fields": {"solutions": {"ts": "solutions", "codec": "solutionsResponse.solutions"}}, "required": ["solutions"]},
  "solutionsResponse.solutions": {"kind": "array", "items": "Solution"},
  "sourcesResponse": {"kind": "object", "fields": {"sources": {"ts": "sources", "codec": "sourcesResponse.sources"}}, "required": ["sources"]},
  "sourcesResponse.sources": {"kind": "array", "items": "Source"},
  "spansResponse": {"kind": "object", "fields": {"length_m": {"ts": "lengthM"}, "route_id": {"ts": "routeId"}, "spans": {"ts": "spans", "codec": "spansResponse.spans"}}, "required": ["length_m", "route_id", "spans"]},
  "spansResponse.spans": {"kind": "array", "items": "Span"},
  "substationsResponse": {"kind": "object", "fields": {"substations": {"ts": "substations", "codec": "substationsResponse.substations"}}, "required": ["substations"]},
  "substationsResponse.substations": {"kind": "array", "items": "Substation"},
  "sweepRunsResponse": {"kind": "object", "fields": {"runs": {"ts": "runs", "codec": "sweepRunsResponse.runs"}}, "required": ["runs"]},
  "sweepRunsResponse.runs": {"kind": "array", "items": "Run"},
  "traceResponse": {"kind": "object", "fields": {"inputs": {"ts": "inputs", "codec": "traceResponse.inputs"}, "producer": {"ts": "producer", "codec": "ModelVersion"}, "reproducible": {"ts": "reproducible"}, "run": {"ts": "run", "codec": "ProvenanceRun"}}, "required": ["inputs", "producer", "reproducible", "run"]},
  "traceResponse.inputs": {"kind": "array", "items": "TracedInput"},
  "valuesResponse": {"kind": "object", "fields": {"issued_at": {"ts": "issuedAt"}, "span_id": {"ts": "spanId"}, "values": {"ts": "values", "codec": "valuesResponse.values"}}, "required": ["issued_at", "span_id", "values"]},
  "valuesResponse.values": {"kind": "array", "items": "SpanValue"},
  "violationsResponse": {"kind": "object", "fields": {"branches": {"ts": "branches", "codec": "violationsResponse.branches"}, "solution_id": {"ts": "solutionId"}, "threshold": {"ts": "threshold"}}, "required": ["branches", "solution_id", "threshold"]},
  "violationsResponse.branches": {"kind": "array"},
};

function codecFor(id?: string): Codec | undefined {
  return id ? CODECS[id] : undefined;
}

// setOwn assigns obj[key] = value via Object.defineProperty rather than
// bracket/dot assignment. This matters for exactly one key: "__proto__".
// obj[key] = value, when key is the literal string "__proto__", goes
// through the legacy Object.prototype.__proto__ ACCESSOR -- it sets obj's
// PROTOTYPE (silently ignoring the assignment entirely if value is not an
// object or null) instead of creating an own data property. A wire (or,
// under NamingPreserve/an override, client) field literally named
// "__proto__" would otherwise silently vanish from the walked result: not
// an error, just an object one property short of what the codec table
// claims it renamed. Object.defineProperty has no such special case for any
// key, "__proto__" included, so it is used unconditionally here rather than
// only when key happens to be "__proto__" -- one code path, not two.
function setOwn(obj: Record<string, unknown>, key: string, value: unknown): void {
  Object.defineProperty(obj, key, { value, writable: true, enumerable: true, configurable: true });
}

function walk(value: unknown, id: string | undefined, toTS: boolean): unknown {
  const codec = codecFor(id);
  if (!codec || value === null || value === undefined) {
    return value;
  }

  switch (codec.kind) {
    case 'object': {
      if (typeof value !== 'object' || Array.isArray(value)) {
        return value;
      }

      const src = value as Record<string, unknown>;
      const out: Record<string, unknown> = {};

      // Build the rename map in the requested direction. Decoding maps a
      // wire key to its ts name; encoding maps back.
      const rename = new Map<string, { to: string; codec?: string | undefined }>();
      for (const [wire, field] of Object.entries(codec.fields)) {
        if (toTS) {
          rename.set(wire, { to: field.ts, codec: field.codec });
        } else {
          rename.set(field.ts, { to: wire, codec: field.codec });
        }
      }

      for (const [key, val] of Object.entries(src)) {
        const mapped = rename.get(key);
        if (mapped) {
          setOwn(out, mapped.to, walk(val, mapped.codec, toTS));
        } else if (codec.values) {
          // Not a declared field, but this object also has a typed
          // additionalProperties value schema (a declared-Properties-AND-
          // additionalProperties schema, rendered as an intersection type):
          // the KEY is data and stays untouched, exactly like the 'record'
          // case below, but the VALUE still needs walking so its own
          // fields get renamed too.
          setOwn(out, key, walk(val, codec.values, toTS));
        } else {
          // Unknown key: pass through verbatim, name and value untouched.
          setOwn(out, key, val);
        }
      }

      return out;
    }

    case 'array': {
      if (!Array.isArray(value)) {
        return value;
      }

      return value.map((item) => walk(item, codec.items, toTS));
    }

    case 'record': {
      if (typeof value !== 'object' || Array.isArray(value)) {
        return value;
      }

      const src = value as Record<string, unknown>;
      const out: Record<string, unknown> = {};

      // Keys are data here, not field names — they are never renamed.
      for (const [key, val] of Object.entries(src)) {
        setOwn(out, key, walk(val, codec.values, toTS));
      }

      return out;
    }

    case 'union': {
      if (typeof value !== 'object' || Array.isArray(value)) {
        return value;
      }

      const src = value as Record<string, unknown>;

      if (codec.discriminator) {
        // codec.discriminator.wire is the WIRE name of the tag property
        // (e.g. "pet_kind"). When decoding (toTS), src IS wire-shaped, so
        // reading it directly is correct -- this is the pre-existing,
        // already-correct half, and is untouched below.
        //
        // When ENCODING (!toTS), src is TS-shaped (e.g. { petKind: 'dog' }):
        // reading src[codec.discriminator.wire] looks up a key that does not
        // exist on a TS-shaped object at all, so a naive single read was
        // always undefined and fell through to the passthrough below --
        // silently shipping the whole union unrenamed. (Fix-round-1 review,
        // CRITICAL.)
        //
        // A first fix tried ONE candidate: the ts name the FIRST member
        // (in declared order) that happens to declare this wire property
        // renders it as. That is correct only when every member agrees on
        // that name -- fix-round-2 review measured two ways it silently
        // breaks:
        //
        //   (a) members DISAGREE on the ts name (e.g. a schema-scoped
        //       FieldOverrides entry renaming only one member's rendering).
        //       The first-declared member's name wins the scan regardless
        //       of which member the payload actually is, so a payload
        //       matching a LATER member under ITS OWN name never resolves;
        //   (b) NO member declares the property at all (a lenient spec --
        //       strict OpenAPI requires it on every member, but nothing
        //       here enforces that). The scan finds nothing, falls back to
        //       the bare wire name, which is virtually never present on a
        //       TS-shaped payload since the property isn't even part of any
        //       member's declared TypeScript type.
        //
        // Fixed by trying every DISTINCT ts name any member declares for
        // this wire property, in declared order, plus the wire name itself
        // as a final fallback (covers a lenient spec where a member
        // legitimately preserves the wire name, e.g. under
        // NamingPreserve/an override that reproduces it exactly). A
        // candidate is accepted only when BOTH it exists as a string key on
        // src AND codec.discriminator.map resolves that string to a real
        // member -- so trying more than one candidate can never invent a
        // match a single-candidate read wouldn't also have found under the
        // right name. If two DIFFERENT candidates each resolve to a
        // DIFFERENT member, the payload is genuinely ambiguous (which
        // member did the caller actually mean?) and this passes through
        // rather than guessing, exactly like the undiscriminated
        // structural match below already does for its own ambiguous case.
        // codecTable.checkDiscriminatorTSNameAgreement (codecs.go) warns at
        // generation time when members disagree on, or entirely omit, this
        // property -- both are real spec smells the caller should see.
        let memberID: string | undefined;

        if (toTS) {
          const tag = src[codec.discriminator.wire];
          memberID = typeof tag === 'string' ? codec.discriminator.map[tag] : undefined;
        } else {
          const candidates: string[] = [];
          const seenCandidates = new Set<string>();
          const addCandidate = (key: string) => {
            if (!seenCandidates.has(key)) {
              seenCandidates.add(key);
              candidates.push(key);
            }
          };

          for (const mID of codec.members ?? []) {
            const m = codecFor(mID);
            const declared = m && m.kind === 'object' ? m.fields[codec.discriminator.wire] : undefined;
            if (declared) {
              addCandidate(declared.ts);
            }
          }
          addCandidate(codec.discriminator.wire);

          let ambiguous = false;

          for (const key of candidates) {
            const candidateTag = src[key];
            if (typeof candidateTag !== 'string') {
              continue;
            }

            const candidateMemberID = codec.discriminator.map[candidateTag];
            if (!candidateMemberID) {
              continue;
            }

            if (memberID === undefined) {
              memberID = candidateMemberID;
            } else if (memberID !== candidateMemberID) {
              ambiguous = true;
            }
          }

          if (ambiguous) {
            memberID = undefined;
          }
        }

        if (!memberID) {
          return value;
        }

        return walk(value, memberID, toTS);
      }

      // No discriminator: try each member in the order it was declared,
      // taking the first whose required fields are ALL present on the
      // value. A member that is evidence-free -- not an 'object' at all, or
      // an object with no required fields -- is SKIPPED, not treated as a
      // vacuous match: an empty required list is satisfied by every object
      // trivially, which would turn that member into an unconditional
      // catch-all rather than a real structural test, defeating "never a
      // best-effort guess" for the common case where a schema simply has no
      // required fields declared (the OpenAPI default). If every member is
      // skipped, fall through to the same passthrough a genuine mismatch
      // gets -- generation-time warns when a member has no evidence to
      // offer (see unionEntry), so this is never a silent surprise.
      for (const memberID of codec.members ?? []) {
        const member = codecFor(memberID);
        if (!member || member.kind !== 'object') {
          continue;
        }

        const required = member.required;
        if (!required || required.length === 0) {
          continue;
        }

        // 'required' lists WIRE names (see requiredWireFields). When
        // decoding, 'src' is wire-shaped, so those names are tested
        // directly -- the pre-existing, already-correct half. When
        // ENCODING, 'src' is TS-shaped: testing a wire name like
        // "bark_volume" against it always failed, silently, because that
        // key never exists on a TS-shaped object -- the same class of bug
        // as the discriminator case above, just for the undiscriminated
        // structural match. (Fix-round-1 review, CRITICAL.) Fixed by
        // mapping each required wire name through this SAME member's own
        // 'fields' table to the ts name it renders as before testing
        // presence on 'src'.
        const keys = toTS ? required : required.map((wireKey) => member.fields[wireKey]?.ts ?? wireKey);

        if (keys.every((key) => Object.prototype.hasOwnProperty.call(src, key))) {
          return walk(value, memberID, toTS);
        }
      }

      // No member matched: pass through verbatim rather than guess.
      return value;
    }

    default:
      return value;
  }
}

/** Convert a wire payload into its TypeScript shape. */
export function decode<T>(value: unknown, codecID?: string): T {
  return walk(value, codecID, true) as T;
}

/** Convert a TypeScript value into its wire shape. */
export function encode(value: unknown, codecID?: string): unknown {
  return walk(value, codecID, false);
}
