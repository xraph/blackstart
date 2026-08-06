// @vitest-environment jsdom
/**
 * The menu.
 *
 * Separate from `Select` because its items are actions rather than a value,
 * and the two navigate differently: focus moves into a menu and stays on the
 * trigger of a listbox. Every assertion below is one of those differences.
 */

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { Menu } from "./menu";

afterEach(cleanup);

function renderMenu(onSignOut = vi.fn()): { onSignOut: ReturnType<typeof vi.fn> } {
  render(
    <Menu
      label="Account"
      trigger={<span>RR</span>}
      header={
        <>
          <p>Rex Raphael</p>
          <p>rex.i.raphael@gmail.com</p>
        </>
      }
      items={[
        { id: "copy", label: "Copy user id", onSelect: vi.fn() },
        { id: "out", label: "Sign out", onSelect: onSignOut },
      ]}
    />,
  );

  return { onSignOut };
}

describe("Menu", () => {
  it("names the trigger rather than leaving it to the initials", () => {
    // "RR" tells a screen reader nothing about what pressing it does.
    renderMenu();

    const trigger = screen.getByRole("button", { name: "Account" });
    expect(trigger.getAttribute("aria-haspopup")).toBe("menu");
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
  });

  it("keeps its items out of the tree while closed", () => {
    renderMenu();

    expect(screen.queryByRole("menuitem")).toBeNull();
  });

  it("moves focus onto the first item when it opens", () => {
    // The difference from a listbox: these are actions, so the reader's caret
    // belongs on the one they are about to take.
    renderMenu();

    fireEvent.click(screen.getByRole("button", { name: "Account" }));

    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Copy user id" }));
  });

  it("roams the items with the arrows and wraps", () => {
    renderMenu();
    const trigger = screen.getByRole("button", { name: "Account" });
    fireEvent.click(trigger);

    const first = screen.getByRole("menuitem", { name: "Copy user id" });
    const last = screen.getByRole("menuitem", { name: "Sign out" });

    fireEvent.keyDown(first, { key: "ArrowDown" });
    expect(document.activeElement).toBe(last);

    fireEvent.keyDown(last, { key: "ArrowDown" });
    expect(document.activeElement).toBe(first);

    fireEvent.keyDown(first, { key: "ArrowUp" });
    expect(document.activeElement).toBe(last);
  });

  it("runs the item and closes", () => {
    const { onSignOut } = renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Account" }));

    fireEvent.click(screen.getByRole("menuitem", { name: "Sign out" }));

    expect(onSignOut).toHaveBeenCalledOnce();
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("returns focus to the trigger on Escape", () => {
    renderMenu();
    const trigger = screen.getByRole("button", { name: "Account" });
    fireEvent.click(trigger);

    fireEvent.keyDown(screen.getByRole("menuitem", { name: "Copy user id" }), { key: "Escape" });

    expect(document.activeElement).toBe(trigger);
  });

  it("carries header content that is not an item", () => {
    // The identity block is text to read, not a thing to activate. Making it
    // a menuitem would put two unreachable stops in the reader's way.
    renderMenu();
    fireEvent.click(screen.getByRole("button", { name: "Account" }));

    expect(screen.getByText("rex.i.raphael@gmail.com")).toBeTruthy();
    expect(screen.getAllByRole("menuitem")).toHaveLength(2);
  });
});
