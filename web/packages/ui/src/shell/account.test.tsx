// @vitest-environment jsdom
/**
 * The identity badge, which used to sign you out on one click.
 *
 * The old control was labelled, so it announced itself; it was not
 * deliberate, which is a different property and the one that matters for an
 * action that ends the session.
 */

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { AccountMenu } from "./account";

afterEach(cleanup);

const SESSION = {
  userId: "usr_01k3qf8m2xdhjk",
  email: "rex.i.raphael@gmail.com",
  name: "Rex Raphael",
};

describe("AccountMenu", () => {
  it("prints initials with nothing behind them when nobody is signed in", () => {
    render(<AccountMenu initials="—" session={null} onSignOut={vi.fn()} />);

    expect(screen.getByText("—")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("does not sign out on a single press of the badge", () => {
    // The whole point of the change: an unlabelled 20px target is a poor
    // place to put the one action that ends the session.
    const onSignOut = vi.fn();
    render(<AccountMenu initials="RR" session={SESSION} onSignOut={onSignOut} />);

    fireEvent.click(screen.getByRole("button", { name: /account/i }));

    expect(onSignOut).not.toHaveBeenCalled();
  });

  it("carries the name, address and user id the badge cannot show", () => {
    render(<AccountMenu initials="RR" session={SESSION} onSignOut={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: /account/i }));

    expect(screen.getByText("Rex Raphael")).toBeTruthy();
    expect(screen.getByText("rex.i.raphael@gmail.com")).toBeTruthy();
    // Monospace and selectable rather than a copy button: it is the field a
    // support conversation asks for, and selecting text needs no permission.
    expect(screen.getByText("usr_01k3qf8m2xdhjk")).toBeTruthy();
  });

  it("falls back to the address when the account has no name", () => {
    render(<AccountMenu initials="RR" session={{ ...SESSION, name: "" }} onSignOut={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: /account/i }));

    // Not an empty line where a name would be: the account genuinely has
    // none, and printing the address twice would say that less clearly.
    expect(screen.queryByText("Rex Raphael")).toBeNull();
    expect(screen.getByText("rex.i.raphael@gmail.com")).toBeTruthy();
  });

  it("signs out from a named item", () => {
    const onSignOut = vi.fn();
    render(<AccountMenu initials="RR" session={SESSION} onSignOut={onSignOut} />);

    fireEvent.click(screen.getByRole("button", { name: /account/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign out" }));

    expect(onSignOut).toHaveBeenCalledOnce();
  });
});
