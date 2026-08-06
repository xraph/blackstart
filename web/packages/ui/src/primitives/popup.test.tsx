// @vitest-environment jsdom
/**
 * The overlay behaviours every control in this package shares.
 *
 * Tested against a bare harness rather than through `Select`, because the
 * claim being made is about the popup and a failure here should not be
 * reported as a listbox bug.
 */

import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { PopupPanel, usePopup } from "./popup";

afterEach(cleanup);

function Harness(): React.JSX.Element {
  const popup = usePopup();

  return (
    <div>
      <button type="button" onClick={() => popup.setOpen(!popup.open)} ref={popup.triggerRef}>
        open
      </button>
      <PopupPanel popup={popup}>
        <button type="button">inside</button>
      </PopupPanel>
      <button type="button">outside</button>
    </div>
  );
}

describe("usePopup", () => {
  it("keeps the panel out of the tree while closed", () => {
    // Not merely hidden: a focusable subtree behind a closed panel is a
    // keyboard trap with no visible target, which is the reason BottomSheet
    // unmounts its body too.
    render(<Harness />);

    expect(screen.queryByRole("button", { name: "inside" })).toBeNull();
  });

  it("opens on the trigger and closes on Escape", () => {
    render(<Harness />);
    const trigger = screen.getByRole("button", { name: "open" });

    fireEvent.click(trigger);
    expect(screen.getByRole("button", { name: "inside" })).toBeTruthy();

    fireEvent.keyDown(trigger, { key: "Escape" });
    expect(screen.queryByRole("button", { name: "inside" })).toBeNull();
  });

  it("returns focus to the trigger when it closes", () => {
    // Without this the reader is left at the top of the document, which is
    // the same as losing their place.
    render(<Harness />);
    const trigger = screen.getByRole("button", { name: "open" });

    trigger.focus();
    fireEvent.click(trigger);
    fireEvent.keyDown(trigger, { key: "Escape" });

    expect(document.activeElement).toBe(trigger);
  });

  it("closes when a pointer goes down outside it", () => {
    render(<Harness />);

    fireEvent.click(screen.getByRole("button", { name: "open" }));
    fireEvent.pointerDown(screen.getByRole("button", { name: "outside" }));

    expect(screen.queryByRole("button", { name: "inside" })).toBeNull();
  });

  it("stays open when a pointer goes down inside it", () => {
    render(<Harness />);

    fireEvent.click(screen.getByRole("button", { name: "open" }));
    fireEvent.pointerDown(screen.getByRole("button", { name: "inside" }));

    expect(screen.getByRole("button", { name: "inside" })).toBeTruthy();
  });

  it("leaves the trigger to toggle itself", () => {
    // The trigger is outside the panel, so an unqualified outside-close would
    // close on pointer-down and the click would reopen — a trigger that never
    // appears to close.
    render(<Harness />);
    const trigger = screen.getByRole("button", { name: "open" });

    fireEvent.click(trigger);
    fireEvent.pointerDown(trigger);
    fireEvent.click(trigger);

    expect(screen.queryByRole("button", { name: "inside" })).toBeNull();
  });
});
