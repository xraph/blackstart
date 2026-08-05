/**
 * Refresh-once-and-replay, around the generated client.
 *
 * The single-flight test is the one that matters. Authsome rotates refresh
 * tokens and runs replay detection that cascade-revokes the whole token
 * family when it meets a stale one, so a stampede of concurrent refreshes
 * signs the operator out rather than keeping them in — which is the opposite
 * of what this exists to do.
 */

import { describe, expect, it, vi } from "vitest";
import { UnauthorizedError } from "@blackstart/api-client";

import { withReauth } from "./reauth";

/**
 * A client whose one method fails with the given statuses in order, then
 * succeeds. Real behaviour rather than a mock's: the call counts asserted
 * below are counts of actual invocations.
 */
function clientFailing(...statuses: number[]) {
  const remaining = [...statuses];
  let calls = 0;

  return {
    calls: () => calls,
    client: {
      read(): Promise<string> {
        calls += 1;
        const status = remaining.shift();

        if (status === 401) return Promise.reject(new UnauthorizedError("expired"));
        if (status !== undefined) return Promise.reject(new Error(`http ${status}`));

        return Promise.resolve("ok");
      },
    },
  };
}

describe("withReauth", () => {
  it("refreshes and replays the call that met a 401", async () => {
    const { client, calls } = clientFailing(401);
    const refresh = vi.fn(() => Promise.resolve());
    const guarded = withReauth(client, refresh, () => undefined);

    await expect(guarded.read()).resolves.toBe("ok");

    expect(refresh).toHaveBeenCalledOnce();
    expect(calls()).toBe(2);
  });

  it("refreshes once for a screenful of calls that expire together", async () => {
    // Twelve panels meeting 401 in the same tick must produce one /refresh.
    const refresh = vi.fn(() => Promise.resolve());
    const guards = Array.from({ length: 12 }, () => {
      const { client } = clientFailing(401);
      return withReauth(client, refresh, () => undefined);
    });

    await Promise.all(guards.map((g) => g.read()));

    expect(refresh).toHaveBeenCalledOnce();
  });

  it("gives up rather than looping when the replay is refused too", async () => {
    const { client, calls } = clientFailing(401, 401);
    const refresh = vi.fn(() => Promise.resolve());
    const guarded = withReauth(client, refresh, () => undefined);

    await expect(guarded.read()).rejects.toBeInstanceOf(UnauthorizedError);

    expect(refresh).toHaveBeenCalledOnce();
    expect(calls()).toBe(2);
  });

  it("leaves an error that is not a 401 alone", async () => {
    const { client, calls } = clientFailing(500);
    const refresh = vi.fn(() => Promise.resolve());
    const guarded = withReauth(client, refresh, () => undefined);

    await expect(guarded.read()).rejects.toThrow("http 500");

    expect(refresh).not.toHaveBeenCalled();
    expect(calls()).toBe(1);
  });

  it("declares the session over when it cannot be refreshed", async () => {
    const { client } = clientFailing(401);
    const expired = vi.fn();
    const guarded = withReauth(client, () => Promise.reject(new Error("session expired")), expired);

    await expect(guarded.read()).rejects.toBeInstanceOf(UnauthorizedError);

    expect(expired).toHaveBeenCalledOnce();
  });

  it("passes a value that is not a method through untouched", async () => {
    const client = { baseURL: "/api/v1", read: () => Promise.resolve("ok") };
    const guarded = withReauth(
      client,
      () => Promise.resolve(),
      () => undefined,
    );

    expect(guarded.baseURL).toBe("/api/v1");
  });

  it("recovers again after an earlier refresh has settled", async () => {
    // The in-flight promise must be cleared once it resolves, or the second
    // expiry an hour later replays against a refresh that already happened.
    const refresh = vi.fn(() => Promise.resolve());
    const first = clientFailing(401);
    const second = clientFailing(401);

    await withReauth(first.client, refresh, () => undefined).read();
    await withReauth(second.client, refresh, () => undefined).read();

    expect(refresh).toHaveBeenCalledTimes(2);
  });
});
