/**
 * The whole path, against a running platform.
 *
 * Sign up, take the token, build a client with it, and pull real geometry.
 * Every layer between the login form and the globe is exercised, which no
 * amount of stubbing does — the stubs assert that each piece behaves as I
 * expect, and this asserts that my expectations match the platform.
 *
 * Skipped unless BLACKSTART_LIVE names a reachable host, so it is a tool for
 * whoever has one running rather than a CI failure for everyone who does not.
 */

import { beforeAll, describe, expect, it } from "vitest";
import { createApiClient } from "@blackstart/api-client";
import { signUp } from "../auth/authsome";
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

describe.runIf(BASE !== "" && PK !== "")("live platform", () => {
  it("authenticates and reads geometry the globe can draw", async () => {
    expect(reachable, `no platform at ${BASE}`).toBe(true);

    // A fresh operator each run: sign-in refuses an unverified address, so
    // registering is how a first session is obtained on a new deployment.
    const email = `probe-${Date.now()}@blackstart.test`;
    const session = await signUp({ email, password: "Correct-Horse-Battery-9" }, PK, BASE);

    expect(session.token, "sign-up returned no session token").not.toBe("");

    const anonymous = createApiClient({ baseUrl: BASE });
    await expect(anonymous.geo.routes.list(1)).rejects.toBeTruthy();

    const client = createApiClient({ baseUrl: BASE, bearerToken: session.token });
    const routes = await client.geo.routes.list(5);

    expect(Array.isArray(routes.routes)).toBe(true);

    if (routes.routes.length === 0) {
      // Reachable, authenticated, and nothing traced yet. Say so rather than
      // passing silently on an empty result that proves nothing.
      throw new Error("authenticated but no routes exist; run `bsctl geo trace` first");
    }

    const route = routes.routes[0];
    expect(route?.branchId, "codec did not rename branch_id -> branchId").toBeDefined();
    expect(route?.lengthM, "codec did not rename length_m -> lengthM").toBeGreaterThan(0);

    const spans = await client.geo.routes.spans(route?.id ?? "");
    expect(spans.spans.length).toBeGreaterThan(0);

    const line = polylineOfSpans(route?.id ?? "", route?.branchId ?? "", spans.spans);
    expect(line, "spans did not assemble into a drawable path").not.toBeNull();
    expect(line?.points.length).toBeGreaterThan(1);

    for (const p of line?.points ?? []) {
      expect(Number.isFinite(p.lat) && Math.abs(p.lat) <= 90).toBe(true);
      expect(Number.isFinite(p.lon) && Math.abs(p.lon) <= 180).toBe(true);
    }
  }, 30_000);
});
