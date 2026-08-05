// @vitest-environment jsdom
/**
 * The three states of being signed in.
 *
 * `checking` is the one worth testing hardest. Collapsing it into `anonymous`
 * flashes the login form at an operator who is already signed in, every time
 * they reload — and it is invisible in development against a warm localhost,
 * where `/me` answers before the first paint.
 *
 * These drive the real identity module through a stubbed `fetch`, so the wire
 * shapes are exercised rather than asserted twice.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";

import { AuthProvider, useAuth } from "./provider";

const PK = "pk_live_test";

/** Answers each identity path from a table, so a test states only what it changes. */
function stubIdentity(routes: Record<string, { status: number; body?: unknown }>): void {
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      const route = routes[url] ?? { status: 404, body: {} };

      return Promise.resolve({
        ok: route.status >= 200 && route.status < 300,
        status: route.status,
        json: () => Promise.resolve(route.body ?? {}),
      } as Response);
    }),
  );
}

/** Renders the state as text, which is all these tests need to read. */
function Probe(): React.JSX.Element {
  const { status, session, signOut } = useAuth();

  return (
    <div>
      <span data-testid="status">{status}</span>
      <span data-testid="email">{session?.email ?? "—"}</span>
      <button type="button" onClick={() => void signOut()}>
        Sign out
      </button>
    </div>
  );
}

function mount(): void {
  render(
    <AuthProvider publishableKey={PK}>
      <Probe />
    </AuthProvider>,
  );
}

beforeEach(() => vi.unstubAllGlobals());
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("AuthProvider", () => {
  it("starts out checking rather than anonymous", async () => {
    stubIdentity({ "/identity/v1/me": { status: 200, body: { id: "ausr_1", email: "op@test" } } });

    mount();

    // Synchronously, before /me has answered anything.
    expect(screen.getByTestId("status").textContent).toBe("checking");

    await waitFor(() => expect(screen.getByTestId("status").textContent).toBe("authenticated"));
  });

  it("adopts the session the cookie already carries", async () => {
    stubIdentity({ "/identity/v1/me": { status: 200, body: { id: "ausr_1", email: "op@test" } } });

    mount();

    await waitFor(() => expect(screen.getByTestId("email").textContent).toBe("op@test"));
  });

  it("settles anonymous when nobody is signed in", async () => {
    stubIdentity({ "/identity/v1/me": { status: 401 } });

    mount();

    await waitFor(() => expect(screen.getByTestId("status").textContent).toBe("anonymous"));
  });

  it("settles anonymous when identity cannot be reached at all", async () => {
    // A host that is down must not strand the console on a splash screen with
    // no way forward. The login form is at least something to act on.
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.reject(new Error("connection refused"))),
    );

    mount();

    await waitFor(() => expect(screen.getByTestId("status").textContent).toBe("anonymous"));
  });

  it("goes anonymous on sign-out", async () => {
    stubIdentity({
      "/identity/v1/me": { status: 200, body: { id: "ausr_1", email: "op@test" } },
      "/identity/v1/signout": { status: 200, body: { status: "signed out" } },
    });

    mount();
    await waitFor(() => expect(screen.getByTestId("status").textContent).toBe("authenticated"));

    screen.getByRole("button", { name: "Sign out" }).click();

    await waitFor(() => expect(screen.getByTestId("status").textContent).toBe("anonymous"));
  });
});
