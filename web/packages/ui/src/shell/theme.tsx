/**
 * Choosing a ground.
 *
 * The tokens already do the work: `:root[data-theme]` overrides the
 * `prefers-color-scheme` block in both directions, and every component reads
 * `bg-panel` rather than a colour. So this sets one attribute and nothing else
 * — a switcher that also swapped classes or re-declared colours would be a
 * second theme mechanism racing the first.
 *
 * Three states, not two. "System" is a real choice and the CSS already
 * implements it: no attribute means the media query decides. Collapsing it to
 * a boolean would make an operator whose OS switches at dusk unable to ask for
 * the same thing.
 */

import { useCallback, useEffect, useState, type ReactElement } from "react";

import { cn } from "../cn";

export type Theme = "system" | "light" | "dark";

const STORAGE_KEY = "blackstart.theme";

function isTheme(value: string | null): value is Theme {
  return value === "system" || value === "light" || value === "dark";
}

/**
 * Reads the stored choice.
 *
 * Wrapped because Safari throws on localStorage in private browsing rather
 * than returning null, and a console that will not render at all because it
 * could not remember a colour is a worse failure than forgetting one.
 */
function stored(): Theme {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return isTheme(raw) ? raw : "system";
  } catch {
    return "system";
  }
}

/** Writes the attribute the tokens key off, or removes it for "system". */
function apply(theme: Theme): void {
  const root = document.documentElement;

  if (theme === "system") root.removeAttribute("data-theme");
  else root.setAttribute("data-theme", theme);
}

export interface ThemeState {
  readonly theme: Theme;
  readonly setTheme: (next: Theme) => void;
}

export function useTheme(): ThemeState {
  const [theme, setThemeState] = useState<Theme>(stored);

  useEffect(() => {
    apply(theme);
  }, [theme]);

  const setTheme = useCallback((next: Theme) => {
    setThemeState(next);

    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // A choice that outlives the tab is a convenience, not a requirement.
    }
  }, []);

  return { theme, setTheme };
}

const ORDER: readonly Theme[] = ["system", "light", "dark"];

const GLYPH: Record<Theme, string> = {
  // Geometric rather than emoji: the chrome is monospace and 9–11px, and an
  // emoji sun renders at a different weight and baseline in every browser.
  system: "◐",
  light: "☀",
  dark: "☾",
};

const LABEL: Record<Theme, string> = {
  system: "Match the system",
  light: "Light",
  dark: "Dark",
};

/**
 * The switcher, as a segmented control.
 *
 * Three buttons rather than one that cycles. A cycling button cannot say what
 * it will do next without being pressed, and the current state has to be
 * inferred from the icon it happens to be showing.
 */
export function ThemeToggle({
  theme,
  onChange,
  className,
}: {
  readonly theme: Theme;
  readonly onChange: (next: Theme) => void;
  readonly className?: string;
}): ReactElement {
  return (
    <div
      role="radiogroup"
      aria-label="Colour theme"
      className={cn(
        "flex items-center gap-px rounded-[3px] border border-line-2 bg-panel-2 p-px",
        className,
      )}
    >
      {ORDER.map((option) => {
        const on = option === theme;

        return (
          <button
            key={option}
            type="button"
            role="radio"
            aria-checked={on}
            title={LABEL[option]}
            onClick={() => onChange(option)}
            className={cn(
              "flex size-[18px] items-center justify-center rounded-[2px] text-[10px] leading-none",
              "focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2",
              on ? "bg-panel-3 text-fg" : "text-fg-3 hover:text-fg-2",
            )}
          >
            <span aria-hidden="true">{GLYPH[option]}</span>
            <span className="sr-only">{LABEL[option]}</span>
          </button>
        );
      })}
    </div>
  );
}
