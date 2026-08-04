/**
 * The route tree.
 *
 * Search params are validated by the route rather than by each screen, so an
 * unparseable URL fails at the boundary instead of three components deep with
 * a blank panel and no explanation.
 */

import { createRoute, createRouter, redirect } from "@tanstack/react-router";
import { ValidTime } from "@blackstart/schema";
import { Route as rootRoute } from "./routes/root";
import { cascadeSearch, consoleSearch, runsSearch } from "./search/params";
import { ConsoleScreen } from "./routes/console";
import { CascadeScreen } from "./routes/cascade";
import { RunsScreen } from "./routes/runs";

/**
 * Where a bare visit lands.
 *
 * A valid time is required rather than defaulted inside the schema, because a
 * console showing "now" without saying which forecast hour it means is the
 * ambiguity the whole time axis exists to remove. The redirect names one.
 */
const LANDING_SEARCH = consoleSearch.parse({
  valid: ValidTime.parse("2026-08-02T22:00Z"),
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: () => {
    // Parsed rather than written out, so the landing URL carries whatever
    // defaults the schema declares instead of a second copy of them.
    throw redirect({ to: "/console", search: LANDING_SEARCH });
  },
});

const consoleRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/console",
  validateSearch: (search: Record<string, unknown>) => consoleSearch.parse(search),
  component: ConsoleScreen,
});

const cascadeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/cascade",
  validateSearch: (search: Record<string, unknown>) => cascadeSearch.parse(search),
  component: CascadeScreen,
});

const runsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/runs",
  validateSearch: (search: Record<string, unknown>) => runsSearch.parse(search),
  component: RunsScreen,
});

export const routeTree = rootRoute.addChildren([indexRoute, consoleRoute, cascadeRoute, runsRoute]);

export const router = createRouter({ routeTree });

/* Registers the router's inferred types globally, which is what makes
 * `navigate({ to: "/console", search: … })` type-checked at call sites. */
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

export { consoleRoute, cascadeRoute, runsRoute };
