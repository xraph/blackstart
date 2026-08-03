import type { RequestConfig } from './fetch';
import { Client } from './client';
import * as types from './types';

export class RESTClient extends Client {
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
      list: async (modelHash?: string, limit?: number | undefined, options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }): Promise<types.runsResponse> => {
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
          responseCodec: "runsResponse",
        };

        return this.request<types.runsResponse>(config);
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

}
