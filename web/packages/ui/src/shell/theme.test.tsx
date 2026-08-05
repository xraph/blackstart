/**
 * The switcher drives the tokens, and nothing else.
 *
 * The whole mechanism is one attribute on <html>: `:root[data-theme]` beats
 * the `prefers-color-scheme` block in both directions. These tests assert that
 * contract rather than any rendered colour — a test that read a computed
 * background would be testing Tailwind, and would pass just as happily if the
 * switcher had grown a second, parallel way to set one.
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { ThemeToggle, useTheme } from "./theme";

function Harness(): React.JSX.Element {
  const { theme, setTheme } = useTheme();

  return (
    <>
      <span data-testid="current">{theme}</span>
      <ThemeToggle theme={theme} onChange={setTheme} />
    </>
  );
}

afterEach(() => {
  cleanup();
  document.documentElement.removeAttribute("data-theme");
  localStorage.clear();
});

/** No jest-dom in this workspace; the assertions read the DOM directly. */
function press(name: string): void {
  fireEvent.click(screen.getByRole("radio", { name }));
}

describe("theme", () => {
  it("leaves the attribute off for system, so the media query decides", () => {
    render(<Harness />);

    press("Dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");

    press("Match the system");
    expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
  });

  it("sets the attribute the tokens key off", () => {
    render(<Harness />);

    press("Light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });

  it("remembers the choice for the next visit", () => {
    const { unmount } = render(<Harness />);

    press("Light");
    unmount();

    render(<Harness />);
    expect(screen.getByTestId("current").textContent).toBe("light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });

  it("ignores a stored value it does not recognise", () => {
    // A hand-edited or stale key must not put an unknown string on <html>,
    // where it would match no token block and silently render the dark
    // defaults while the control claimed something else.
    localStorage.setItem("blackstart.theme", "sepia");

    render(<Harness />);
    expect(screen.getByTestId("current").textContent).toBe("system");
    expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
  });

  it("says which ground is current, not merely which are available", () => {
    render(<Harness />);

    // A cycling button could not answer this without being pressed.
    expect(
      screen.getByRole("radio", { name: "Match the system" }).getAttribute("aria-checked"),
    ).toBe("true");
    expect(screen.getByRole("radio", { name: "Dark" }).getAttribute("aria-checked")).toBe("false");
  });
});
