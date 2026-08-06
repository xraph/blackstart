// @vitest-environment jsdom
/**
 * The listbox.
 *
 * Every assertion here is a behaviour the `▾` glyphs this replaces did not
 * have. They are written out rather than left to a library because §2 of the
 * primitives design accepted the keyboard model as ours to get right.
 */

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { Select } from "./select";

afterEach(cleanup);

const CASES = [
  { value: "pegase-2869", label: "PEGASE 2869", meta: "8f2c1ab0" },
  { value: "activsg-2000", label: "ACTIVSg 2000", meta: "3d90e7c4" },
  { value: "ieee-118", label: "IEEE 118", meta: "c1e40b62" },
] as const;

function open(): HTMLElement {
  const trigger = screen.getByRole("combobox");
  fireEvent.click(trigger);
  return trigger;
}

function active(trigger: HTMLElement): string | null {
  const id = trigger.getAttribute("aria-activedescendant");
  return id === null ? null : (document.getElementById(id)?.textContent ?? null);
}

describe("Select", () => {
  it("states the current value and that it is collapsed", () => {
    render(<Select label="case" value="ieee-118" options={CASES} onChange={vi.fn()} />);

    const trigger = screen.getByRole("combobox", { name: /case/i });

    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(trigger.textContent).toContain("IEEE 118");
  });

  it("says so when nothing is chosen rather than naming something arbitrary", () => {
    // Two operators opening the same bare URL on different days must not see
    // different scenarios.
    render(
      <Select
        label="scenario"
        value={undefined}
        options={CASES}
        onChange={vi.fn()}
        placeholder="— none —"
      />,
    );

    expect(screen.getByRole("combobox").textContent).toContain("— none —");
  });

  it("opens a listbox whose options report which is selected", () => {
    render(<Select label="case" value="ieee-118" options={CASES} onChange={vi.fn()} />);
    const trigger = open();

    expect(trigger.getAttribute("aria-expanded")).toBe("true");

    const options = screen.getAllByRole("option");
    expect(options).toHaveLength(3);
    expect(options[2]?.getAttribute("aria-selected")).toBe("true");
    expect(options[0]?.getAttribute("aria-selected")).toBe("false");
  });

  it("starts active on the selected option, not the first", () => {
    render(<Select label="case" value="ieee-118" options={CASES} onChange={vi.fn()} />);
    const trigger = open();

    expect(active(trigger)).toContain("IEEE 118");
  });

  it("moves the active option with the arrows and wraps at both ends", () => {
    render(<Select label="case" value="pegase-2869" options={CASES} onChange={vi.fn()} />);
    const trigger = open();

    fireEvent.keyDown(trigger, { key: "ArrowDown" });
    expect(active(trigger)).toContain("ACTIVSg 2000");

    fireEvent.keyDown(trigger, { key: "ArrowUp" });
    expect(active(trigger)).toContain("PEGASE 2869");

    // Wrapping matters more here than in a long list: three options means the
    // reader is never more than one press from any of them.
    fireEvent.keyDown(trigger, { key: "ArrowUp" });
    expect(active(trigger)).toContain("IEEE 118");

    fireEvent.keyDown(trigger, { key: "ArrowDown" });
    expect(active(trigger)).toContain("PEGASE 2869");
  });

  it("jumps to the ends with Home and End", () => {
    render(<Select label="case" value="activsg-2000" options={CASES} onChange={vi.fn()} />);
    const trigger = open();

    fireEvent.keyDown(trigger, { key: "End" });
    expect(active(trigger)).toContain("IEEE 118");

    fireEvent.keyDown(trigger, { key: "Home" });
    expect(active(trigger)).toContain("PEGASE 2869");
  });

  it("finds an option by typing at it", () => {
    render(<Select label="case" value="pegase-2869" options={CASES} onChange={vi.fn()} />);
    const trigger = open();

    fireEvent.keyDown(trigger, { key: "i" });
    expect(active(trigger)).toContain("IEEE 118");
  });

  it("commits the active option on Enter and closes", () => {
    const onChange = vi.fn();
    render(<Select label="case" value="pegase-2869" options={CASES} onChange={onChange} />);
    const trigger = open();

    fireEvent.keyDown(trigger, { key: "ArrowDown" });
    fireEvent.keyDown(trigger, { key: "Enter" });

    expect(onChange).toHaveBeenCalledExactlyOnceWith("activsg-2000");
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("commits on click and reports the value once", () => {
    const onChange = vi.fn();
    render(<Select label="case" value="pegase-2869" options={CASES} onChange={onChange} />);
    open();

    fireEvent.click(screen.getByRole("option", { name: /IEEE 118/ }));

    expect(onChange).toHaveBeenCalledExactlyOnceWith("ieee-118");
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("opens onto the first option when the arrow is what opened it", () => {
    render(<Select label="scenario" value={undefined} options={CASES} onChange={vi.fn()} />);
    const trigger = screen.getByRole("combobox");

    fireEvent.keyDown(trigger, { key: "ArrowDown" });

    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(active(trigger)).toContain("PEGASE 2869");
  });

  it("abandons the choice on Escape", () => {
    const onChange = vi.fn();
    render(<Select label="case" value="pegase-2869" options={CASES} onChange={onChange} />);
    const trigger = open();

    fireEvent.keyDown(trigger, { key: "ArrowDown" });
    fireEvent.keyDown(trigger, { key: "Escape" });

    expect(screen.queryByRole("listbox")).toBeNull();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("renders nothing to choose from as a disabled trigger", () => {
    // An empty list that opens is a panel saying nothing. The trigger says it
    // instead, and cannot be opened onto emptiness.
    render(<Select label="case" value={undefined} options={[]} onChange={vi.fn()} />);

    const trigger = screen.getByRole("combobox");
    expect(trigger.hasAttribute("disabled")).toBe(true);

    fireEvent.click(trigger);
    expect(screen.queryByRole("listbox")).toBeNull();
  });
});
