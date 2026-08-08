import type { RequestConfig } from './fetch';
import { Client } from './client';
import * as types from './types';

export class RESTClient extends Client {
  public readonly cascade = {
    runs: {
      /**
       * Get a cascade run's extent
       * The corridor area within half_width_m (default 500, in meters on each side of a traced route) of every branch the run tripped. Corridors that overlap are returned whole rather than merged, so this is corridor area, not a single figure of distinct ground — see shape.Cover.
       */
      extent: async (id: string, halfWidthM?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.extentResponse> => {
        let __path = `/api/v1/cascade/runs/${encodeURIComponent(String(id))}/extent`;
        const queryParams: Record<string, any> = {};
        if (halfWidthM !== undefined) {
          queryParams['half_width_m'] = halfWidthM;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "extentResponse",
        };

        return this.request<types.extentResponse>(config);
      },

      /**
       * Get a cascade run
       * One recorded simulation, with every event in the order it happened in simulated time.
       */
      get: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.CascadeRun> => {
        let __path = `/api/v1/cascade/runs/${encodeURIComponent(String(id))}`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "CascadeRun",
        };

        return this.request<types.CascadeRun>(config);
      },

      /**
       * Get a cascade run's final islands
       * The islanded state the network settled, truncated, or collapsed into, with each island's worst condition over the run and its final AC verdict.
       */
      islands: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.islandsResponse> => {
        let __path = `/api/v1/cascade/runs/${encodeURIComponent(String(id))}/islands`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "islandsResponse",
        };

        return this.request<types.islandsResponse>(config);
      },

      /**
       * List cascades for a network
       * Every recorded cascade simulation of one network model, most recent first.
       */
      list: async (modelHash?: string, limit?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.cascadeRunsResponse> => {
        let __path = `/api/v1/cascade/runs`;
        const queryParams: Record<string, any> = {};
        queryParams['modelHash'] = modelHash;
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "cascadeRunsResponse",
        };

        return this.request<types.cascadeRunsResponse>(config);
      },

    },

  };

  public readonly contingency = {
    runs: {
      /**
       * Get a sweep
       * One recorded analysis, with every contingency in the order it was evaluated. That order is preserved because it is what was asked for; ranking is a separate view.
       */
      get: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.Run> => {
        let __path = `/api/v1/contingency/runs/${encodeURIComponent(String(id))}`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "Run",
        };

        return this.request<types.Run>(config);
      },

      /**
       * List sweeps for a network
       * Every recorded N-1 analysis of one network model, most recent first.
       */
      list: async (modelHash?: string, limit?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.sweepRunsResponse> => {
        let __path = `/api/v1/contingency/runs`;
        const queryParams: Record<string, any> = {};
        queryParams['modelHash'] = modelHash;
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "sweepRunsResponse",
        };

        return this.request<types.sweepRunsResponse>(config);
      },

      /**
       * Rank a sweep's contingencies
       * Worst first, with contingencies the screen could not evaluate ahead of everything else. A radial outage that islands part of the network is usually the most consequential thing on the list and is exactly what a linear factor cannot answer — ranking it by a loading it does not have would bury it.
       */
      ranked: async (id: string, all?: boolean | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.rankedResponse> => {
        let __path = `/api/v1/contingency/runs/${encodeURIComponent(String(id))}/ranked`;
        const queryParams: Record<string, any> = {};
        if (all !== undefined) {
          queryParams['all'] = all;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "rankedResponse",
        };

        return this.request<types.rankedResponse>(config);
      },

    },

  };

  public readonly datasource = {
    feeds: {
      /**
       * Get a feed
       */
      get: async (id: string, validAt?: string | undefined, knownAt?: string | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.Feed> => {
        let __path = `/api/v1/datasources/feeds/${encodeURIComponent(String(id))}`;
        const queryParams: Record<string, any> = {};
        if (validAt !== undefined) {
          queryParams['validAt'] = validAt;
        }
        if (knownAt !== undefined) {
          queryParams['knownAt'] = knownAt;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "Feed",
        };

        return this.request<types.Feed>(config);
      },

      /**
       * List a source's feeds
       * The retrievable resources within one data source.
       */
      list: async (id: string, limit?: number | undefined, after?: string | undefined, validAt?: string | undefined, knownAt?: string | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.feedsResponse> => {
        let __path = `/api/v1/datasources/${encodeURIComponent(String(id))}/feeds`;
        const queryParams: Record<string, any> = {};
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        if (after !== undefined) {
          queryParams['after'] = after;
        }
        if (validAt !== undefined) {
          queryParams['validAt'] = validAt;
        }
        if (knownAt !== undefined) {
          queryParams['knownAt'] = knownAt;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "feedsResponse",
        };

        return this.request<types.feedsResponse>(config);
      },

    },

    sources: {
      /**
       * Get a data source
       * Accepts either an identifier or a slug. A caller holding a slug from a document and one holding an identifier from an API response are asking the same question.
       */
      get: async (id: string, validAt?: string | undefined, knownAt?: string | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.Source> => {
        let __path = `/api/v1/datasources/${encodeURIComponent(String(id))}`;
        const queryParams: Record<string, any> = {};
        if (validAt !== undefined) {
          queryParams['validAt'] = validAt;
        }
        if (knownAt !== undefined) {
          queryParams['knownAt'] = knownAt;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "Source",
        };

        return this.request<types.Source>(config);
      },

      /**
       * List data sources
       * The public data source registry. Every dataset blackstart consumes traces to an entry here, and every provenance record points at one.
       */
      list: async (kind?: string | undefined, slug?: string | undefined, limit?: number | undefined, after?: string | undefined, validAt?: string | undefined, knownAt?: string | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.sourcesResponse> => {
        let __path = `/api/v1/datasources`;
        const queryParams: Record<string, any> = {};
        if (kind !== undefined) {
          queryParams['kind'] = kind;
        }
        if (slug !== undefined) {
          queryParams['slug'] = slug;
        }
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        if (after !== undefined) {
          queryParams['after'] = after;
        }
        if (validAt !== undefined) {
          queryParams['validAt'] = validAt;
        }
        if (knownAt !== undefined) {
          queryParams['knownAt'] = knownAt;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "sourcesResponse",
        };

        return this.request<types.sourcesResponse>(config);
      },

    },

  };

  public readonly geo = {
    branches: {
      /**
       * Find the route carrying a branch
       */
      route: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.Route> => {
        let __path = `/api/v1/geo/branches/${encodeURIComponent(String(id))}/route`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "Route",
        };

        return this.request<types.Route>(config);
      },

    },

    circuits: {
      /**
       * Get a circuit
       */
      get: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.Circuit> => {
        let __path = `/api/v1/geo/circuits/${encodeURIComponent(String(id))}`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "Circuit",
        };

        return this.request<types.Circuit>(config);
      },

      /**
       * List imported circuits
       * Transmission lines imported from public sources. These carry no electrical parameters and cannot appear in a power flow or a contingency; a route naming one is geometry only.
       */
      list: async (limit?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.circuitsResponse> => {
        let __path = `/api/v1/geo/circuits`;
        const queryParams: Record<string, any> = {};
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "circuitsResponse",
        };

        return this.request<types.circuitsResponse>(config);
      },

      /**
       * Find the route of a circuit
       */
      route: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.Route> => {
        let __path = `/api/v1/geo/circuits/${encodeURIComponent(String(id))}/route`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "Route",
        };

        return this.request<types.Route>(config);
      },

    },

    routes: {
      /**
       * Get a route
       * A branch's path over the ground, with the provenance of that path: surveyed means a source recorded where the structures actually are, interpolated means a straight great-circle line with evenly spaced imaginary ones. A per-span failure probability conditioned on the second is a different claim from one conditioned on the first.
       */
      get: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.Route> => {
        let __path = `/api/v1/geo/routes/${encodeURIComponent(String(id))}`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "Route",
        };

        return this.request<types.Route>(config);
      },

      /**
       * List traced routes
       */
      list: async (limit?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.routesResponse> => {
        let __path = `/api/v1/geo/routes`;
        const queryParams: Record<string, any> = {};
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "routesResponse",
        };

        return this.request<types.routesResponse>(config);
      },

      /**
       * Get a route's spans in order
       * Geometry is per span because failure is per span: a conductor breaks between two structures, an ice load is local, and a 200 km line passes through several weather systems at once.
       */
      spans: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.spansResponse> => {
        let __path = `/api/v1/geo/routes/${encodeURIComponent(String(id))}/spans`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "spansResponse",
        };

        return this.request<types.spansResponse>(config);
      },

    },

  };

  public readonly hazard = {
    assignments: {
      /**
       * Which conductor each carrier is strung with
       * Every assignment carries a basis. A surveyed assignment and one assumed from the voltage class are different claims, and a rating conditioned on the second is indicative rather than authoritative.
       */
      list: async (limit?: number | undefined, after?: string | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.assignmentsResponse> => {
        let __path = `/api/v1/hazard/assignments`;
        const queryParams: Record<string, any> = {};
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        if (after !== undefined) {
          queryParams['after'] = after;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "assignmentsResponse",
        };

        return this.request<types.assignmentsResponse>(config);
      },

    },

    computations: {
      /**
       * Passes of the models over a weather cycle, newest first
       * Each names the cycle it read and the provenance run it produced, and counts the spans it rated against the spans it skipped for want of a conductor assignment.
       */
      list: async (limit?: number | undefined, after?: string | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.computationsResponse> => {
        let __path = `/api/v1/hazard/computations`;
        const queryParams: Record<string, any> = {};
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        if (after !== undefined) {
          queryParams['after'] = after;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "computationsResponse",
        };

        return this.request<types.computationsResponse>(config);
      },

    },

    conductors: {
      /**
       * The conductor library
       * Standard ACSR types with the properties IEEE 738 needs: diameter, resistance at two temperatures, emissivity, absorptivity and the maximum operating temperature a rating is the current at.
       */
      list: async (options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.conductorsResponse> => {
        let __path = `/api/v1/hazard/conductors`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "conductorsResponse",
        };

        return this.request<types.conductorsResponse>(config);
      },

    },

    spans: {
      /**
       * A span's mechanical loading over a window
       * Wind drag and ice weight in newtons per meter, ice thickness in meters, ice mass in kilograms per meter, and the resultant of drag and weight. Absent quantities are absent rather than zero: GFS publishes no freezing-rain accumulation, so a GFS-derived pass has no ice series at all.
       */
      loading: async (id: string, issued?: string, from?: string, to?: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.valuesResponse> => {
        let __path = `/api/v1/hazard/spans/${encodeURIComponent(String(id))}/loading`;
        const queryParams: Record<string, any> = {};
        queryParams['issued'] = issued;
        queryParams['from'] = from;
        queryParams['to'] = to;
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "valuesResponse",
        };

        return this.request<types.valuesResponse>(config);
      },

      /**
       * A span's thermal rating over a window
       * Amperes, from IEEE 738's steady-state heat balance evaluated at the conductor's temperature limit. Issuance is a parameter rather than inferred, because "what is the rating now" and "what did the 06Z run say the rating would be" are different questions, and only the second can be checked afterwards.
       */
      rating: async (id: string, issued?: string, from?: string, to?: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.valuesResponse> => {
        let __path = `/api/v1/hazard/spans/${encodeURIComponent(String(id))}/rating`;
        const queryParams: Record<string, any> = {};
        queryParams['issued'] = issued;
        queryParams['from'] = from;
        queryParams['to'] = to;
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "valuesResponse",
        };

        return this.request<types.valuesResponse>(config);
      },

    },

  };

  public readonly networkmodel = {
    /**
     * Get a model's canonical encoding
     * The bytes the solver reads, exactly as stored. The content hash is the sha256 of these bytes, so anyone can verify that a result citing that hash was computed against this network and not another.
     */
    encoding: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.encodingResponse> => {
      let __path = `/api/v1/models/${encodeURIComponent(String(id))}/encoding`;
      const config: RequestConfig = {
        method: 'GET',
        url: __path,
        signal: options?.signal,
        retry: options?.retry,
        responseCodec: "encodingResponse",
      };

      return this.request<types.encodingResponse>(config);
    },

    /**
     * Get a network model by identifier or content hash
     */
    get: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.NetworkModel> => {
      let __path = `/api/v1/models/${encodeURIComponent(String(id))}`;
      const config: RequestConfig = {
        method: 'GET',
        url: __path,
        signal: options?.signal,
        retry: options?.retry,
        responseCodec: "NetworkModel",
      };

      return this.request<types.NetworkModel>(config);
    },

    /**
     * List stored network models
     * Every network this platform can be asked about, most recent first. A model is immutable: correcting one produces a new model with a new content hash, so a result citing the old hash keeps meaning what it meant.
     */
    list: async (limit?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.modelsResponse> => {
      let __path = `/api/v1/models`;
      const queryParams: Record<string, any> = {};
      if (limit !== undefined) {
        queryParams['limit'] = limit;
      }
      const queryString = new URLSearchParams(queryParams).toString();
      if (queryString) {
        __path += '?' + queryString;
      }
      const config: RequestConfig = {
        method: 'GET',
        url: __path,
        signal: options?.signal,
        retry: options?.retry,
        responseCodec: "modelsResponse",
      };

      return this.request<types.modelsResponse>(config);
    },

  };

  public readonly powerflow = {
    solutions: {
      /**
       * Get a solution
       * One solved power flow, with the solver version and model hash that produced it and whether reactive limits were enforced.
       */
      get: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.Solution> => {
        let __path = `/api/v1/powerflow/solutions/${encodeURIComponent(String(id))}`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "Solution",
        };

        return this.request<types.Solution>(config);
      },

      /**
       * List solutions for a network
       * Every recorded solve of one network model, most recent first. The model is named by content hash, so two solves of the same network are found together whatever it was called.
       */
      list: async (modelHash?: string, limit?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.solutionsResponse> => {
        let __path = `/api/v1/powerflow/solutions`;
        const queryParams: Record<string, any> = {};
        queryParams['modelHash'] = modelHash;
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "solutionsResponse",
        };

        return this.request<types.solutionsResponse>(config);
      },

      /**
       * Branches over their rating
       * Branch indices loaded above a threshold, worst first. Unrated branches are never reported: a zero rating means the limit is unknown, not zero.
       */
      violations: async (id: string, threshold?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.violationsResponse> => {
        let __path = `/api/v1/powerflow/solutions/${encodeURIComponent(String(id))}/violations`;
        const queryParams: Record<string, any> = {};
        if (threshold !== undefined) {
          queryParams['threshold'] = threshold;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "violationsResponse",
        };

        return this.request<types.violationsResponse>(config);
      },

    },

  };

  public readonly provenance = {
    datasets: {
      /**
       * Get a dataset version
       * One retrieval of one external dataset: where it came from, the sha256 of what arrived, and the parser version that read it.
       */
      get: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.DatasetVersion> => {
        let __path = `/api/v1/provenance/datasets/${encodeURIComponent(String(id))}`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "DatasetVersion",
        };

        return this.request<types.DatasetVersion>(config);
      },

    },

    models: {
      /**
       * Get a producer version
       * A solver build, a parser, or a trained model, with the commit it was built from and the parameters that change its answer.
       */
      get: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.ModelVersion> => {
        let __path = `/api/v1/provenance/models/${encodeURIComponent(String(id))}`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "ModelVersion",
        };

        return this.request<types.ModelVersion>(config);
      },

    },

    runs: {
      /**
       * Trace a run
       * What produced this number: the run, the producer version and the commit it was built from, and every dataset version it read with the role each played. One call rather than a graph walk.
       */
      trace: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.traceResponse> => {
        let __path = `/api/v1/provenance/runs/${encodeURIComponent(String(id))}`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "traceResponse",
        };

        return this.request<types.traceResponse>(config);
      },

    },

  };

  public readonly risk = {
    components: {
      /**
       * A component's failure probability over a window
       * Per-hour P(failure): a span's conductor probability or a structure's, whichever the identifier names. Issuance is a parameter rather than inferred, the same reason hazard's span endpoints require it — a window this API guessed would give the same request a different answer tomorrow.
       */
      series: async (id: string, issued?: string, from?: string, to?: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.seriesResponse> => {
        let __path = `/api/v1/risk/components/${encodeURIComponent(String(id))}/series`;
        const queryParams: Record<string, any> = {};
        queryParams['issued'] = issued;
        queryParams['from'] = from;
        queryParams['to'] = to;
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "seriesResponse",
        };

        return this.request<types.seriesResponse>(config);
      },

    },

  };

  public readonly scenario = {
    runs: {
      /**
       * Queue a run of a scenario
       * Freezes a copy of the scenario's manifest into a new run and returns it queued. A worker claims and executes it; poll the run for its status and, once complete, the sweep and cascade it produced.
       */
      enqueue: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.ScenarioRun> => {
        let __path = `/api/v1/scenario/scenarios/${encodeURIComponent(String(id))}/runs`;
        const config: RequestConfig = {
          method: 'POST',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "ScenarioRun",
        };

        return this.request<types.ScenarioRun>(config);
      },

      /**
       * Get a scenario run
       * One run: the manifest it froze, where it is in its lifecycle, the step it failed at if it failed, and the sweep and cascade it produced if it completed.
       */
      get: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.ScenarioRun> => {
        let __path = `/api/v1/scenario/runs/${encodeURIComponent(String(id))}`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "ScenarioRun",
        };

        return this.request<types.ScenarioRun>(config);
      },

      /**
       * List a scenario's runs
       * Every recorded run of one scenario, most recent first.
       */
      list: async (id: string, limit?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.scenarioRunsResponse> => {
        let __path = `/api/v1/scenario/scenarios/${encodeURIComponent(String(id))}/runs`;
        const queryParams: Record<string, any> = {};
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "scenarioRunsResponse",
        };

        return this.request<types.scenarioRunsResponse>(config);
      },

      /**
       * List runs across every scenario
       * Every recorded run, most recent first, whatever scenario it belongs to. This is what a run index reads; a single scenario's history is /scenarios/{id}/runs.
       */
      recent: async (limit?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.scenarioRunsResponse> => {
        let __path = `/api/v1/scenario/runs`;
        const queryParams: Record<string, any> = {};
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "scenarioRunsResponse",
        };

        return this.request<types.scenarioRunsResponse>(config);
      },

    },

    scenarios: {
      /**
       * Define or revise a scenario
       * Records the binding a run will execute: a network model by content hash, a forecast cycle and the moment within it, a contingency policy and a seed. Naming an existing scenario revises it, which changes what the next run executes and nothing about any run already recorded.
       */
      define: async (body: Record<string, any>, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.Scenario> => {
        let __path = `/api/v1/scenario/scenarios`;
        const config: RequestConfig = {
          method: 'POST',
          url: __path,
          body,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "Scenario",
        };

        return this.request<types.Scenario>(config);
      },

      /**
       * Get a scenario
       * One scenario and the manifest it currently binds.
       */
      get: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.Scenario> => {
        let __path = `/api/v1/scenario/scenarios/${encodeURIComponent(String(id))}`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "Scenario",
        };

        return this.request<types.Scenario>(config);
      },

      /**
       * List scenarios
       * Defined scenarios, most recently created first.
       */
      list: async (limit?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.scenariosResponse> => {
        let __path = `/api/v1/scenario/scenarios`;
        const queryParams: Record<string, any> = {};
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "scenariosResponse",
        };

        return this.request<types.scenariosResponse>(config);
      },

    },

  };

  public readonly scl = {
    devices: {
      /**
       * What stops working if this device is lost
       * The communication graph around one device: who subscribes to it, what it subscribes to, and what it publishes. The first is the question the electrical model cannot answer at all — two relays on the same bus may exchange nothing, and two relays in different substations may be tightly coupled.

Advisory. Nothing here commands anything; it reports what a configuration file says.
       */
      impact: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.impactResponse> => {
        let __path = `/api/v1/scl/devices/${encodeURIComponent(String(id))}/impact`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "impactResponse",
        };

        return this.request<types.impactResponse>(config);
      },

      /**
       * List configured devices
       * Every intelligent electronic device an imported configuration describes, by the name every external reference resolves against.
       */
      list: async (limit?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.devicesResponse> => {
        let __path = `/api/v1/scl/devices`;
        const queryParams: Record<string, any> = {};
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "devicesResponse",
        };

        return this.request<types.devicesResponse>(config);
      },

    },

  };

  public readonly topology = {
    substations: {
      /**
       * Get a substation
       */
      get: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.Substation> => {
        let __path = `/api/v1/topology/substations/${encodeURIComponent(String(id))}`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "Substation",
        };

        return this.request<types.Substation>(config);
      },

      /**
       * List substations
       * Every site in the network model.
       */
      list: async (limit?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.substationsResponse> => {
        let __path = `/api/v1/topology/substations`;
        const queryParams: Record<string, any> = {};
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "substationsResponse",
        };

        return this.request<types.substationsResponse>(config);
      },

      /**
       * Reduce a substation to bus-branch form
       * Collapses every set of connectivity nodes joined by closed zero-impedance devices into one bus. Switch positions come from each device's recorded normal state; open and closed override individual devices, which is how a study asks what would happen if one breaker moved. Derived on every call and never stored — two models of one network drift, and the drift stays invisible until a result disagrees with a diagram.
       */
      reduction: async (id: string, open?: string[] | undefined, closed?: string[] | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.reductionResponse> => {
        let __path = `/api/v1/topology/substations/${encodeURIComponent(String(id))}/reduction`;
        const queryParams: Record<string, any> = {};
        if (open !== undefined) {
          queryParams['open'] = open;
        }
        if (closed !== undefined) {
          queryParams['closed'] = closed;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "reductionResponse",
        };

        return this.request<types.reductionResponse>(config);
      },

    },

  };

  public readonly weather = {
    cycles: {
      /**
       * Get one model run
       */
      get: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.Cycle> => {
        let __path = `/api/v1/weather/cycles/${encodeURIComponent(String(id))}`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "Cycle",
        };

        return this.request<types.Cycle>(config);
      },

      /**
       * List a run's retrievals
       * One entry per field per forecast hour, each naming the bytes it was decoded from and the dataset version that recorded the retrieval. Covered and total say how many spans fell inside the grid's domain — HRRR is CONUS-only, so a span outside it has no sample rather than a zero.
       */
      grids: async (id: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.gridsResponse> => {
        let __path = `/api/v1/weather/cycles/${encodeURIComponent(String(id))}/grids`;
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "gridsResponse",
        };

        return this.request<types.gridsResponse>(config);
      },

      /**
       * List model runs, newest issuance first
       */
      list: async (model?: string | undefined, status?: string | undefined, limit?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.cyclesResponse> => {
        let __path = `/api/v1/weather/cycles`;
        const queryParams: Record<string, any> = {};
        if (model !== undefined) {
          queryParams['model'] = model;
        }
        if (status !== undefined) {
          queryParams['status'] = status;
        }
        if (limit !== undefined) {
          queryParams['limit'] = limit;
        }
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "cyclesResponse",
        };

        return this.request<types.cyclesResponse>(config);
      },

    },

    spans: {
      /**
       * What a run expected at one span
       * Issuance is a parameter rather than inferred from the window, because "what does the latest run say" and "what did the 06Z run say" are different questions — and a reader who cannot ask the second cannot check a forecast against what happened.
       */
      forecast: async (id: string, field?: string | undefined, issued?: string, from?: string, to?: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.forecastResponse> => {
        let __path = `/api/v1/weather/spans/${encodeURIComponent(String(id))}/forecast`;
        const queryParams: Record<string, any> = {};
        if (field !== undefined) {
          queryParams['field'] = field;
        }
        queryParams['issued'] = issued;
        queryParams['from'] = from;
        queryParams['to'] = to;
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "forecastResponse",
        };

        return this.request<types.forecastResponse>(config);
      },

    },

    stations: {
      /**
       * What a station measured
       */
      observations: async (id: string, field?: string | undefined, from?: string, to?: string, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.observationsResponse> => {
        let __path = `/api/v1/weather/stations/${encodeURIComponent(String(id))}/observations`;
        const queryParams: Record<string, any> = {};
        if (field !== undefined) {
          queryParams['field'] = field;
        }
        queryParams['from'] = from;
        queryParams['to'] = to;
        const queryString = new URLSearchParams(queryParams).toString();
        if (queryString) {
          __path += '?' + queryString;
        }
        const config: RequestConfig = {
          method: 'GET',
          url: __path,
          signal: options?.signal,
          retry: options?.retry,
          responseCodec: "observationsResponse",
        };

        return this.request<types.observationsResponse>(config);
      },

    },

  };

}
