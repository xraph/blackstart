/**
 * The platform connection.
 *
 * One QueryClient and one API client for the application. The generated hooks
 * take the client as an argument rather than reading a singleton, so this
 * context exists to save every screen threading it down — the hooks stay
 * usable from a test without any of this.
 *
 * Retries are off. A forecast console showing a stale answer three times over
 * is worse than one showing an error once: the operator needs to know the
 * number in front of them is not current, and a silent retry hides exactly
 * that. The exception is a genuine network blip, which the browser already
 * retries beneath us.
 */

import { createContext, useContext, useMemo, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createApiClient } from "@blackstart/api-client";

type ApiClient = ReturnType<typeof createApiClient>;

const ApiContext = createContext<ApiClient | null>(null);

export interface ApiProviderProps {
  /** Absolute only for development against a host elsewhere; same-origin otherwise. */
  readonly baseUrl?: string;
  readonly children: ReactNode;
}

export function ApiProvider({ baseUrl, children }: ApiProviderProps): React.JSX.Element {
  const client = useMemo(
    () => (baseUrl === undefined ? createApiClient() : createApiClient({ baseUrl })),
    [baseUrl],
  );

  const queryClient = useMemo(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            retry: false,
            // A forecast cycle is hourly, so a minute of freshness costs
            // nothing and stops every remount refetching the same answer.
            staleTime: 60_000,
            refetchOnWindowFocus: false,
          },
        },
      }),
    [],
  );

  return (
    <QueryClientProvider client={queryClient}>
      <ApiContext.Provider value={client}>{children}</ApiContext.Provider>
    </QueryClientProvider>
  );
}

/**
 * The configured client, for passing to a generated hook.
 *
 * Throws rather than returning null: a screen that reached this without a
 * provider above it is a wiring mistake, and a null client would surface as an
 * empty panel with no explanation.
 */
export function useApiClient(): ApiClient {
  const client = useContext(ApiContext);

  if (client === null) {
    throw new Error("useApiClient: no ApiProvider above this component");
  }

  return client;
}
