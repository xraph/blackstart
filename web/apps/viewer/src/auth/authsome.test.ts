/**
 * The identity contract, as verified against a running host.
 *
 * These pin the details that are easy to get subtly wrong and impossible to
 * notice: the publishable key must be a header rather than a body field, the
 * session lives in a cookie the page cannot read, and `/me` answering 401 is
 * the anonymous answer rather than a failure.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityError, me, refresh, signIn, signOut, signUp } from "./authsome";

const PK = "pk_live_test";

/** The shape signin and signup return: a user, alongside tokens this ignores. */
const AUTH_BODY = {
  session_token: "tok",
  refresh_token: "ref",
  expires_at: "2026-08-04T00:00:00Z",
  user: { id: "ausr_1", email: "op@test" },
};

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

/** The (url, init) of the nth call, typed. */
function callOf(fn: ReturnType<typeof vi.fn>, n = 0): [string, RequestInit] {
  return fn.mock.calls[n] as [string, RequestInit];
}

beforeEach(() => vi.unstubAllGlobals());
afterEach(() => vi.unstubAllGlobals());

describe("identity", () => {
  it("sends the publishable key as a header, not in the body", async () => {
    const fetchMock = stubFetch(200, AUTH_BODY);

    await signIn({ email: "op@test", password: "pw" }, PK);

    const [, init] = callOf(fetchMock);
    const headers = init.headers as Record<string, string>;

    expect(headers["X-Publishable-Key"]).toBe(PK);
    expect(String(init.body)).not.toContain(PK);
  });

  it("returns an identity carrying no credential", async () => {
    // The session cookie is HttpOnly. Anything the page could hold would be a
    // second copy of a credential it is not supposed to have, so the returned
    // value has nowhere to put one.
    stubFetch(200, AUTH_BODY);

    const session = await signIn({ email: "op@test", password: "pw" }, PK);

    expect(session).toEqual({
      userId: "ausr_1",
      email: "op@test",
      firstName: "",
      lastName: "",
    });
  });

  it("signs up against the platform's own registration path", async () => {
    const fetchMock = stubFetch(201, AUTH_BODY);

    const session = await signUp({ email: "new@test", password: "pw" }, PK);

    const [url] = callOf(fetchMock);
    expect(url).toBe("/identity/v1/signup");
    expect(session.email).toBe("op@test");
  });

  // Authsome models first_name and last_name as first-class fields on the
  // signup body, not as custom form fields. Sending them under those exact
  // snake_case keys is the whole contract — camelCase would be silently
  // dropped by `omitempty` binding and the account would simply have no name.
  it("registers with the operator's name under the platform's own keys", async () => {
    const fetchMock = stubFetch(201, AUTH_BODY);

    await signUp(
      { email: "new@test", password: "pw", firstName: "Rex", lastName: "Raphael" },
      PK,
    );

    const [, init] = callOf(fetchMock);
    const body = JSON.parse((init as RequestInit).body as string) as Record<string, string>;

    expect(body["first_name"]).toBe("Rex");
    expect(body["last_name"]).toBe("Raphael");
  });

  // A blank string is a name the account then has, and it would render as
  // whitespace in the identity badge rather than falling back to the address.
  it("omits a name it was not given rather than sending an empty one", async () => {
    const fetchMock = stubFetch(201, AUTH_BODY);

    await signUp({ email: "new@test", password: "pw", firstName: "  ", lastName: "" }, PK);

    const [, init] = callOf(fetchMock);
    const body = JSON.parse((init as RequestInit).body as string) as Record<string, string>;

    expect("first_name" in body).toBe(false);
    expect("last_name" in body).toBe(false);
  });

  it("reads the name back off the user the platform returns", async () => {
    stubFetch(201, {
      ...AUTH_BODY,
      user: { id: "ausr_1", email: "op@test", first_name: "Rex", last_name: "Raphael" },
    });

    const session = await signUp({ email: "new@test", password: "pw" }, PK);

    expect(session.firstName).toBe("Rex");
    expect(session.lastName).toBe("Raphael");
  });

  // Sign-in takes no name, and must not start sending one just because the
  // shared helper learned how.
  it("keeps the sign-in body to a credential", async () => {
    const fetchMock = stubFetch(200, AUTH_BODY);

    await signIn({ email: "op@test", password: "pw" }, PK);

    const [, init] = callOf(fetchMock);
    const body = JSON.parse((init as RequestInit).body as string) as Record<string, string>;

    expect(Object.keys(body).sort()).toEqual(["email", "password"]);
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

  it("refuses a 2xx that names no user", async () => {
    // A response with no user is not a session. Failing here beats reporting
    // an operator signed in as nobody and discovering it three screens later.
    stubFetch(200, { session_token: "tok" });

    await expect(signIn({ email: "op@test", password: "pw" }, PK)).rejects.toBeInstanceOf(
      IdentityError,
    );
  });
});

describe("me", () => {
  it("reads the current user from the bare /me shape", async () => {
    // `/me` returns a user.User at the top level, where signin wraps one under
    // `user`. Reading it with the wrong shape yields a session with an empty
    // email rather than an error, which is why this is pinned.
    const fetchMock = stubFetch(200, { id: "ausr_7", email: "op@test", email_verified: true });

    const session = await me();

    const [url, init] = callOf(fetchMock);
    expect(url).toBe("/identity/v1/me");
    expect(init.method).toBe("GET");
    expect(session).toEqual({
      userId: "ausr_7",
      email: "op@test",
      firstName: "",
      lastName: "",
    });
  });

  it("answers null when nobody is signed in", async () => {
    // 401 here is the answer to the question, not a failure to answer it.
    // Throwing would put an error panel in front of a first-time visitor.
    stubFetch(401, { error: "authentication required" });

    await expect(me()).resolves.toBeNull();
  });

  it("still throws when the platform is broken rather than anonymous", async () => {
    stubFetch(503, { error: "authentication is unavailable" });

    await expect(me()).rejects.toBeInstanceOf(IdentityError);
  });
});

describe("signOut", () => {
  it("asks the platform to delete the cookie", async () => {
    // Clearing local state alone would leave a live session on the host that
    // the next page load silently resumes.
    const fetchMock = stubFetch(200, { status: "signed out" });

    await signOut();

    const [url, init] = callOf(fetchMock);
    expect(url).toBe("/identity/v1/signout");
    expect(init.method).toBe("POST");
  });

  it("does not fail the sign-out when the session was already gone", async () => {
    // The operator asked to be signed out and locally they are. Reporting a
    // failure for a session that is already absent is a distinction without a
    // difference to the person reading it.
    stubFetch(401, { error: "authentication required" });

    await expect(signOut()).resolves.toBeUndefined();
  });
});

describe("refresh", () => {
  it("rotates without sending a token the page does not have", async () => {
    // An empty object, not an empty body. The platform binds the request
    // struct before the handler runs, so a bodyless POST fails at
    // "failed to decode JSON: EOF" and never reaches the cookie-first branch
    // that makes this work — verified against a running host.
    const fetchMock = stubFetch(200, AUTH_BODY);

    await refresh();

    const [url, init] = callOf(fetchMock);
    expect(url).toBe("/identity/v1/refresh");
    expect(init.method).toBe("POST");
    expect(init.body).toBe("{}");
    expect((init.headers as Record<string, string>)["Content-Type"]).toBe("application/json");
  });

  it("rejects when the session cannot be rotated", async () => {
    stubFetch(401, { error: "session expired", type: "session_expired" });

    await expect(refresh()).rejects.toBeInstanceOf(IdentityError);
  });
});
