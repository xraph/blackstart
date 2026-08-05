/**
 * The whole path, against a running platform.
 *
 * Register, take the cookie the platform sets, and pull real geometry with it.
 * The stubs elsewhere assert that each piece behaves as I expect; this asserts
 * that my expectations match the platform — and the central claim of the
 * cookie design is one only a running host can settle: that the `/api/v1`
 * guard accepts a session presented as a cookie rather than as a bearer token.
 *
 * It also answers the question the design left open, by observing rather than
 * assuming: whether sign-up issues a usable session on this deployment's
 * email-verification setting.
 *
 * The identity calls here are raw `fetch` rather than `../auth/authsome`.
 * That module is same-origin by construction — no base URL, because the
 * browser never needs one — and giving it one for the benefit of a test would
 * be test-only configuration in production code. What is verified instead is
 * the contract that module is written against.
 *
 * Node has no cookie jar, so the `Set-Cookie` is carried by hand. In a browser
 * none of this is necessary, which is the point: what has to be arranged
 * manually here is what `fetch` does on its own there.
 *
 * Skipped unless BLACKSTART_LIVE names a reachable host, so it is a tool for
 * whoever has one running rather than a CI failure for everyone who does not.
 */

import { beforeAll, describe, expect, it } from "vitest";
import { RESTClient } from "@blackstart/api-client";
import { polylineOfSpans } from "./use-route-geometry";

/*
 * Read narrowly rather than by adding @types/node. This is a browser app, and
 * pulling Node's globals into its type environment to reach two variables in
 * one test file would make `process` look available to every other file too.
 */
const env =
  (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env ?? {};

const BASE = env["BLACKSTART_LIVE"] ?? "";
const PK = env["BLACKSTART_PK"] ?? "";

let reachable = false;

beforeAll(async () => {
  if (BASE === "" || PK === "") return;

  reachable = await fetch(`${BASE}/livez`)
    .then((r) => r.ok)
    .catch(() => false);
});

/** The `name=value` pair of a Set-Cookie, without its attributes. */
function pairOf(setCookie: string): string {
  return setCookie.split(";")[0] ?? "";
}

describe.runIf(BASE !== "" && PK !== "")("live platform", () => {
  it("issues a session as an HttpOnly cookie the grid API accepts", async () => {
    expect(reachable, `no platform at ${BASE}`).toBe(true);

    // A fresh operator each run: sign-in refuses an unverified address, so
    // registering is how a first session is obtained on a new deployment.
    const email = `probe-${Date.now()}@blackstart.test`;
    const signup = await fetch(`${BASE}/identity/v1/signup`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Publishable-Key": PK },
      body: JSON.stringify({ email, password: "Correct-Horse-Battery-9" }),
    });

    expect(signup.ok, `sign-up returned ${signup.status}`).toBe(true);

    const setCookie = signup.headers.get("set-cookie") ?? "";

    // If this is empty, sign-up withheld the session pending email
    // verification — the asymmetry the login screen's register mode is
    // written around. That is a finding about the deployment, not a bug here,
    // and it must be stated rather than surfacing as a confusing 401 below.
    expect(
      setCookie,
      "sign-up set no session cookie: this deployment withholds a session " +
        "until the address is verified, so the login screen's register mode " +
        "cannot bootstrap a first operator",
    ).not.toBe("");

    expect(setCookie.toLowerCase(), "the session cookie is reachable from JavaScript").toContain(
      "httponly",
    );

    const cookie = pairOf(setCookie);

    // Without the cookie the guard must refuse. A pass here would mean the
    // API is open and every other assertion in this file proves nothing.
    const anonymous = new RESTClient({ baseURL: BASE });
    await expect(anonymous.geo.routes.list(1)).rejects.toBeTruthy();

    // With it, the same call must succeed — the claim the whole design rests
    // on. `customHeaders` stands in for the cookie jar a browser has and Node
    // does not.
    const client = new RESTClient({ baseURL: BASE, auth: { customHeaders: { Cookie: cookie } } });
    const routes = await client.geo.routes.list(5);

    expect(Array.isArray(routes.routes)).toBe(true);

    if (routes.routes.length === 0) {
      // Reachable, authenticated, and nothing traced yet. Say so rather than
      // passing silently on an empty result that proves nothing.
      throw new Error("authenticated but no routes exist; run `bsctl geo trace` first");
    }

    const route = routes.routes[0];
    expect(route?.carrierId, "codec did not rename carrier_id -> carrierId").toBeDefined();
    expect(route?.lengthM, "codec did not rename length_m -> lengthM").toBeGreaterThan(0);

    const spans = await client.geo.routes.spans(route?.id ?? "");
    expect(spans.spans.length).toBeGreaterThan(0);

    const line = polylineOfSpans(route?.id ?? "", route?.carrierId ?? "", spans.spans);
    expect(line, "spans did not assemble into a drawable path").not.toBeNull();
    expect(line?.spans.length).toBeGreaterThan(0);

    for (const p of line?.spans.flatMap((s) => s.points) ?? []) {
      expect(Number.isFinite(p.lat) && Math.abs(p.lat) <= 90).toBe(true);
      expect(Number.isFinite(p.lon) && Math.abs(p.lon) <= 180).toBe(true);
    }
  }, 30_000);

  it("resolves every assignment to a route with spans", async () => {
    // The trap this layer is shaped around. A rating belongs to a carrier, and
    // a carrier is not the route the console draws — it is what the route
    // carries. Looking span ids up against the routes directly colours nothing
    // and reads as a rendering fault rather than as a missing join.
    expect(reachable, `no platform at ${BASE}`).toBe(true);

    const email = `probe-${Date.now()}-hazard@blackstart.test`;
    const signup = await fetch(`${BASE}/identity/v1/signup`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Publishable-Key": PK },
      body: JSON.stringify({ email, password: "Correct-Horse-Battery-9" }),
    });

    const cookie = pairOf(signup.headers.get("set-cookie") ?? "");
    const client = new RESTClient({ baseURL: BASE, auth: { customHeaders: { Cookie: cookie } } });

    const { assignments } = await client.hazard.assignments.list();

    if (assignments.length === 0) {
      throw new Error("no conductor assignments exist; nothing can rate, so nothing can be drawn");
    }

    // Every assignment must resolve to a route, or its ratings exist and
    // cannot be drawn.
    for (const assignment of assignments) {
      const route = await client.geo.branches.route(assignment.carrierId);

      expect(route.id, `carrier ${assignment.carrierId} has a conductor but no route`).toBeTruthy();

      const { spans } = await client.geo.routes.spans(route.id);
      expect(spans.length, "a rateable route with no spans cannot carry a value").toBeGreaterThan(
        0,
      );
    }

    // The counts the panel reports have to come from somewhere real. A pass
    // that rates nothing is a finding about the deployment, not a pass.
    const { computations } = await client.hazard.computations.list();
    expect(computations.length, "no hazard pass has run").toBeGreaterThan(0);
    expect(computations[0]?.spans, "the last pass rated no spans").toBeGreaterThan(0);
  }, 30_000);

  it("rotates the session without a token from the page", async () => {
    expect(reachable, `no platform at ${BASE}`).toBe(true);

    const email = `probe-${Date.now()}-refresh@blackstart.test`;
    const signup = await fetch(`${BASE}/identity/v1/signup`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Publishable-Key": PK },
      body: JSON.stringify({ email, password: "Correct-Horse-Battery-9" }),
    });

    const cookie = pairOf(signup.headers.get("set-cookie") ?? "");
    expect(cookie, "sign-up set no session cookie").not.toBe("");

    // An empty object, not an empty body: the request struct is bound before
    // the handler runs, so a bodyless POST 400s at "failed to decode JSON:
    // EOF" without ever reaching the cookie-first branch. This test is what
    // found that, and it is why it stays.
    const rotated = await fetch(`${BASE}/identity/v1/refresh`, {
      method: "POST",
      headers: { Cookie: cookie, "Content-Type": "application/json" },
      body: "{}",
    });

    expect(rotated.ok, `refresh returned ${rotated.status}`).toBe(true);
  }, 30_000);
});
