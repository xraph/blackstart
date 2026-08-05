// @vitest-environment jsdom
/**
 * The register screen, on both sides of the active flag.
 *
 * The branch is the whole feature: a deployment that declares nothing gets the
 * form this console has always shown, and one that declares fields gets them
 * without a release. Both paths have to create an account, so both are tested
 * against what actually reaches the wire.
 */

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { LoginScreen } from "./LoginScreen";
import type { Registration } from "./authsome";

const signUp = vi.fn<(r: Registration) => Promise<void>>(() => Promise.resolve());

vi.mock("./provider", () => ({
  useAuth: () => ({ signIn: vi.fn(() => Promise.resolve()), signUp }),
}));

/** Answers the contract dispatch, and nothing else. */
function stubConfig(data: unknown): void {
  vi.stubGlobal(
    "fetch",
    vi.fn(() =>
      Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ ok: true, data }),
      } as Response),
    ),
  );
}

async function openRegister(): Promise<void> {
  render(<LoginScreen />);
  fireEvent.click(screen.getByRole("button", { name: "Register an operator instead" }));
}

function fillCredential(): void {
  fireEvent.change(screen.getByLabelText("First name"), { target: { value: "Rex" } });
  fireEvent.change(screen.getByLabelText("Last name"), { target: { value: "Raphael" } });
  fireEvent.change(screen.getByLabelText("Email"), { target: { value: "op@grid.test" } });
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: "correct horse" } });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  signUp.mockClear();
});

describe("register", () => {
  it("registers with no metadata when the deployment declares none", async () => {
    stubConfig({ active: false });
    await openRegister();

    fillCredential();
    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    await waitFor(() => expect(signUp).toHaveBeenCalled());

    const sent = signUp.mock.calls[0]?.[0];
    expect(sent?.firstName).toBe("Rex");
    // Not an empty object: an absent key and an empty map are different
    // things to a server that validates against a config.
    expect(sent?.metadata).toBeUndefined();
  });

  it("renders declared fields and sends them as metadata", async () => {
    stubConfig({
      active: true,
      title: "Join the control room",
      fields: [
        { key: "control_area", label: "Control area", type: "text", order: 1 },
        {
          key: "shift",
          label: "Shift",
          type: "select",
          order: 2,
          options: [{ label: "Nights", value: "nights" }],
        },
      ],
    });

    await openRegister();

    // The declaration drives the heading too, not only the inputs.
    await waitFor(() => expect(screen.getByText("Join the control room")).toBeTruthy());

    fillCredential();
    fireEvent.change(screen.getByLabelText(/Control area/), { target: { value: "ERCOT" } });
    fireEvent.change(screen.getByLabelText(/Shift/), { target: { value: "nights" } });
    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    await waitFor(() => expect(signUp).toHaveBeenCalled());

    expect(signUp.mock.calls[0]?.[0].metadata).toEqual({
      control_area: "ERCOT",
      shift: "nights",
    });
  });

  it("refuses a required field before it reaches the host", async () => {
    stubConfig({
      active: true,
      fields: [
        {
          key: "control_area",
          label: "Control area",
          type: "text",
          order: 1,
          validation: { required: true },
        },
      ],
    });

    await openRegister();
    await waitFor(() => expect(screen.getByLabelText(/Control area/)).toBeTruthy());

    fillCredential();
    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    // The message names the box, and no account was attempted.
    await waitFor(() => expect(screen.getByText("Control area is required")).toBeTruthy());
    expect(signUp).not.toHaveBeenCalled();
  });

  it("clears a message once the box is corrected", async () => {
    stubConfig({
      active: true,
      fields: [
        {
          key: "control_area",
          label: "Control area",
          type: "text",
          order: 1,
          validation: { required: true },
        },
      ],
    });

    await openRegister();
    await waitFor(() => expect(screen.getByLabelText(/Control area/)).toBeTruthy());

    fillCredential();
    fireEvent.click(screen.getByRole("button", { name: "Register" }));
    await waitFor(() => expect(screen.getByText("Control area is required")).toBeTruthy());

    fireEvent.change(screen.getByLabelText(/Control area/), { target: { value: "ERCOT" } });

    // Leaving it under a box that has since been fixed is worse than never
    // having shown one.
    expect(screen.queryByText("Control area is required")).toBeNull();
  });

  it("still registers when the config cannot be fetched", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.reject(new Error("connection refused"))),
    );

    await openRegister();

    fillCredential();
    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    await waitFor(() => expect(signUp).toHaveBeenCalled());
  });

  it("never asks for the form while signing in", async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ ok: true, data: { active: false } }),
      } as Response),
    );

    vi.stubGlobal("fetch", fetchMock);
    render(<LoginScreen />);

    expect(fetchMock).not.toHaveBeenCalled();
  });
});
