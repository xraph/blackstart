// @vitest-environment jsdom
/**
 * The identity contract, as verified against a running host.
 *
 * These pin the details that are easy to get subtly wrong and impossible to
 * notice: the publishable key must be a header rather than a body field, and a
 * 2xx carrying no token is not a session.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityError, signIn, signUp } from "./authsome";

const PK = "pk_live_test";

function stubFetch(status: number, body: unknown): ReturnType<typeof vi.fn> {
  const fn = vi.fn(() =>
    Promise.resolve({
      ok: status >= 200 && status < 300,
      status,
      json: () => Promise.resolve(body),
    } as Response),
  );
  vi.stubGlobal("fetch", fn);
  return fn;
}

beforeEach(() => vi.unstubAllGlobals());
afterEach(() => vi.unstubAllGlobals());

describe("identity", () => {
  it("sends the publishable key as a header, not in the body", async () => {
    const fetchMock = stubFetch(200, {
      session_token: "tok",
      refresh_token: "ref",
      expires_at: "2026-08-04T00:00:00Z",
      user: { id: "ausr_1", email: "op@test" },
    });

    await signIn({ email: "op@test", password: "pw" }, PK);

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const headers = init.headers as Record<string, string>;

    expect(headers["X-Publishable-Key"]).toBe(PK);
    expect(String(init.body)).not.toContain(PK);
  });

  it("returns the session token the platform issued", async () => {
    stubFetch(201, {
      session_token: "tok_abc",
      refresh_token: "ref_abc",
      expires_at: "2026-08-04T00:00:00Z",
      user: { id: "ausr_1", email: "op@test" },
    });

    const session = await signUp({ email: "op@test", password: "pw" }, PK);

    expect(session.token).toBe("tok_abc");
    expect(session.email).toBe("op@test");
  });

  it("carries the platform's own reason for a refusal", async () => {
    stubFetch(403, {
      code: 403,
      error: "please verify your email address before signing in",
      type: "email_not_verified",
    });

    await expect(signIn({ email: "op@test", password: "pw" }, PK)).rejects.toMatchObject({
      status: 403,
      kind: "email_not_verified",
    });
  });

  it("refuses a 2xx that carries no token", async () => {
    // Storing an empty credential turns a broken response into a 401 three
    // screens later, with nothing pointing back at the sign-in that failed.
    stubFetch(200, { user: { id: "ausr_1" } });

    await expect(signIn({ email: "op@test", password: "pw" }, PK)).rejects.toBeInstanceOf(
      IdentityError,
    );
  });
});
