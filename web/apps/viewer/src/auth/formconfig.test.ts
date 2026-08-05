/**
 * The declared signup form, and the ways it can be absent.
 *
 * The load-bearing behaviour is the fallback: registration must work when the
 * dashboard is not mounted, the host is unreachable, or the config is inactive
 * — which is every deployment that has not declared custom fields, i.e. almost
 * all of them. A screen that refused to render because an optional enhancement
 * failed to load would be a worse outcome than never having the enhancement.
 */

import { afterEach, describe, expect, it, vi } from "vitest";

import { initialValues, signupForm, validate, type FormField } from "./formconfig";

function stub(status: number, body: unknown): ReturnType<typeof vi.fn> {
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

const FIELD = (over: Partial<FormField> = {}): FormField => ({
  key: "control_area",
  label: "Control area",
  type: "text",
  order: 1,
  ...over,
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("signupForm", () => {
  it("asks the contract as a query, so no CSRF token is needed", async () => {
    const fetchMock = stub(200, { ok: true, data: { active: false } });

    await signupForm();

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/_forge/dashboard/api/dashboard/v1");

    const body = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(body["kind"]).toBe("query");
    expect(body["contributor"]).toBe("auth");
    expect(body["intent"]).toBe("auth.dynamicConfig");
  });

  it("returns the declared fields in the order the platform set", async () => {
    stub(200, {
      ok: true,
      data: {
        active: true,
        title: "Create your account",
        fields: [FIELD({ key: "b", order: 2 }), FIELD({ key: "a", order: 1 })],
      },
    });

    const form = await signupForm();

    expect(form.active).toBe(true);
    expect(form.fields.map((f) => f.key)).toEqual(["a", "b"]);
    expect(form.title).toBe("Create your account");
  });

  it("treats an inactive config as nothing to add", async () => {
    stub(200, { ok: true, data: { active: false } });

    expect((await signupForm()).active).toBe(false);
  });

  it("falls back when the host cannot be reached", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.reject(new Error("connection refused"))),
    );

    // Not a throw. Registration still has to work.
    expect((await signupForm()).active).toBe(false);
  });

  it("falls back when the dashboard is not mounted", async () => {
    stub(404, {});

    expect((await signupForm()).active).toBe(false);
  });

  it("falls back when the contract refuses the intent", async () => {
    stub(200, { ok: false, error: { code: "NOT_FOUND", message: "intent not registered" } });

    expect((await signupForm()).active).toBe(false);
  });

  // Rendering a repeating group as a text box would collect a string the
  // server stores under a key meant to hold structure — a field that looks
  // answered and is wrong.
  it("drops fields it cannot render honestly", async () => {
    stub(200, {
      ex: 1,
      ok: true,
      data: {
        active: true,
        fields: [
          FIELD({ key: "certs", type: "object_array", order: 1 }),
          FIELD({ key: "area", order: 2 }),
        ],
      },
    });

    const form = await signupForm();

    expect(form.fields.map((f) => f.key)).toEqual(["area"]);
  });

  it("is inactive when nothing renderable survives", async () => {
    stub(200, {
      ok: true,
      data: { active: true, fields: [FIELD({ type: "object_array" })] },
    });

    // Active with no usable field is the same to the screen as inactive, and
    // saying so keeps an empty section from appearing under the password.
    expect((await signupForm()).active).toBe(false);
  });
});

describe("initialValues", () => {
  it("starts a boolean at false rather than absent", () => {
    // Left empty it would submit as unanswered, which for a required boolean
    // is not the same as "no".
    expect(initialValues([FIELD({ key: "on_call", type: "switch" })])).toEqual({
      on_call: "false",
    });
  });

  it("honours a declared default", () => {
    expect(initialValues([FIELD({ default: "ERCOT" })])).toEqual({ control_area: "ERCOT" });
  });
});

describe("validate", () => {
  it("names the field rather than the key", () => {
    const errors = validate([FIELD({ validation: { required: true } })], { control_area: "" });

    expect(errors["control_area"]).toBe("Control area is required");
  });

  it("lets an optional field be left blank", () => {
    expect(validate([FIELD({ validation: { min_len: 3 } })], { control_area: "" })).toEqual({});
  });

  it("applies length and pattern the way the host does", () => {
    const fields = [FIELD({ validation: { min_len: 4, pattern: "^[A-Z]+$" } })];

    expect(validate(fields, { control_area: "ab" })["control_area"]).toContain("at least 4");
    expect(validate(fields, { control_area: "lower" })["control_area"]).toContain("invalid format");
    expect(validate(fields, { control_area: "ERCOT" })).toEqual({});
  });

  it("bounds a number by its declared range", () => {
    const fields = [FIELD({ type: "number", validation: { min: 1, max: 9 } })];

    expect(validate(fields, { control_area: "0" })["control_area"]).toContain("at least 1");
    expect(validate(fields, { control_area: "10" })["control_area"]).toContain("at most 9");
    expect(validate(fields, { control_area: "x" })["control_area"]).toContain("must be a number");
    expect(validate(fields, { control_area: "5" })).toEqual({});
  });

  // The host compiles the pattern too and skips it when it will not compile.
  // Matching that keeps a bad pattern from locking everyone out of signing up.
  it("ignores a pattern this engine cannot compile", () => {
    const fields = [FIELD({ validation: { pattern: "([unclosed" } })];

    expect(validate(fields, { control_area: "anything" })).toEqual({});
  });
});
