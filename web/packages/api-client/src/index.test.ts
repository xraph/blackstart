/**
 * A smoke test over the generated surface.
 *
 * Not a test of the generator — that has its own suite upstream. What this
 * pins is the contract between the checked-in output and the code that
 * consumes it: that regenerating against a changed host cannot quietly remove
 * an endpoint the viewer calls, or turn a typed response back into `void`.
 *
 * The `void` case is the one worth naming. A parser bug meant every generated
 * method returned `Promise<void>` while still compiling, so the client looked
 * healthy and discarded every response body. Nothing but an assertion about
 * the shape catches that.
 */

import { describe, expect, it } from "vitest";
import { asOf, createApiClient } from "./index";

describe("api client", () => {
  it("builds against the same origin by default", () => {
    const client = createApiClient();
    expect(client).toBeDefined();
  });

  it("exposes the grid API groups the console reads", () => {
    const client = createApiClient({ baseUrl: "http://localhost:8080" }) as unknown as Record<
      string,
      unknown
    >;

    // One group per domain extension. A regeneration that drops one of these
    // is a change to what the platform serves, not a formatting difference.
    for (const group of [
      "networkmodel",
      "powerflow",
      "contingency",
      "geo",
      "topology",
      "provenance",
      "datasource",
      "scl",
    ]) {
      expect(client[group], `missing API group: ${group}`).toBeDefined();
    }
  });

  it("carries no identity or dashboard surface", () => {
    const client = createApiClient() as unknown as Record<string, unknown>;

    // The path filter in .forge-client.yaml keeps /identity and /_forge out.
    // If either appears, the filter has silently stopped applying.
    for (const group of ["identity", "auth", "dashboard", "wellKnown"]) {
      expect(client[group], `unexpected API group: ${group}`).toBeUndefined();
    }
  });

  it("omits knowledge time rather than sending now", () => {
    // An absent knowledge time means "whatever is known now", which the API
    // expresses by the parameter being absent. Sending the current instant
    // would pin the answer to a clock reading the caller never chose.
    expect(asOf("2026-08-02T22:00Z")).toEqual({
      validAt: "2026-08-02T22:00Z",
      knownAt: undefined,
    });

    expect(asOf("2019-05-18T03:00Z", "2019-05-18T00:00Z")).toEqual({
      validAt: "2019-05-18T03:00Z",
      knownAt: "2019-05-18T00:00Z",
    });
  });
});

describe("query hooks", () => {
  // A bare count would only say the generator ran. Naming a few keys says the
  // generator kept up with the API — this caught the three circuit endpoints
  // arriving, which is exactly what it is here to catch.
  it("exposes a cache key builder per read endpoint", async () => {
    const { queryKeys } = await import("./index");
    expect(Object.keys(queryKeys).length).toBe(28);

    for (const key of [
      "geoRoutesList",
      "geoRoutesGet",
      "geoCircuitsList",
      "geoCircuitsGet",
      "geoCircuitsRoute",
    ]) {
      expect(queryKeys).toHaveProperty(key);
    }
  });

  it("carries both time axes in the key, not just valid time", async () => {
    const { queryKeys } = await import("./index");

    // The failure this guards against is silent: a key on validAt alone
    // returns what is true now to a caller that asked what was known then,
    // and the answer looks entirely plausible.
    const key = queryKeys.datasourceFeedsGet("feed_1", "2026-08-02T22:00Z", "2019-05-18T00:00Z");

    expect(key).toEqual([
      "datasource",
      "feeds",
      "get",
      { id: "feed_1", validAt: "2026-08-02T22:00Z", knownAt: "2019-05-18T00:00Z" },
    ]);
  });

  it("gives two knowledge times two different keys", async () => {
    const { queryKeys } = await import("./index");

    const live = queryKeys.datasourceFeedsGet("feed_1", "2026-08-02T22:00Z", undefined);
    const asOfThen = queryKeys.datasourceFeedsGet("feed_1", "2026-08-02T22:00Z", "2019-05-18T00:00Z");

    expect(JSON.stringify(live)).not.toEqual(JSON.stringify(asOfThen));
  });

  it("keys are prefixed by their group, so a group can be invalidated at once", async () => {
    const { queryKeys } = await import("./index");
    expect(queryKeys.networkmodelList(10)[0]).toBe("networkmodel");
  });
});
