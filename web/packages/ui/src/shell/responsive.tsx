/**
 * Narrow-viewport shell pieces.
 *
 * The rule is reflow, never scale — an instrument interface loses its meaning
 * when the numbers shrink, so the type scale and row density are fixed and
 * only the layout moves. A docked rail becomes a sheet; an icon rail becomes
 * a tab bar; a twelve-column grid becomes a card list carrying the four
 * columns a reader actually scans.
 */

import { useEffect, useState, type ReactElement, type ReactNode } from "react";
import { cn } from "../cn";
import type { NavItem } from "./app-shell";

/**
 * A media query as state.
 *
 * Exists so a layout can *choose* which of two structures to mount rather
 * than mounting both and hiding one with CSS. Duplicating a subtree is not
 * free — the hidden copy still renders, still holds state, and on a list of
 * sparklines still does the work — and it puts two copies of every control in
 * the document, which assistive technology and tests both trip over.
 */
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(false);

  useEffect(() => {
    const mq = window.matchMedia(query);
    const sync = (): void => setMatches(mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  }, [query]);

  return matches;
}

/** The width at which a docked rail fits beside the stage. Matches Tailwind's `md`. */
export const RAIL_BREAKPOINT = "(min-width: 768px)";

/**
 * Whether the primary pointer is coarse.
 *
 * Gated on pointer rather than viewport width, deliberately. A touch laptop
 * needs the larger target at 1440px and a mouse-driven narrow window does
 * not, so width is a poor proxy for how the thing is actually being touched.
 * Callers use this to raise interactive row heights past the ~44px touch
 * minimum while leaving the 32px pointer density alone.
 */
export function useCoarsePointer(): boolean {
  return useMediaQuery("(pointer: coarse)");
}

export interface BottomSheetProps {
  readonly title: string;
  /** A one-line summary shown while collapsed, so the sheet says what is under it. */
  readonly peek?: ReactNode;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly children: ReactNode;
}

/**
 * A rail, reflowed for a narrow viewport.
 *
 * The body is unmounted rather than merely hidden when collapsed. Leaving it
 * in the tree keeps its controls focusable, which is a keyboard trap with no
 * visible target — the reader tabs into a panel they cannot see.
 */
export function BottomSheet({
  title,
  peek,
  open,
  onOpenChange,
  children,
}: BottomSheetProps): ReactElement {
  return (
    <div
      className={cn(
        "absolute inset-x-0 bottom-0 z-20 flex flex-col rounded-t-lg border-t border-line-2 bg-panel",
        "transition-[max-height] duration-200 motion-reduce:transition-none",
        open ? "max-h-[72%]" : "max-h-24",
      )}
    >
      <button
        type="button"
        aria-expanded={open}
        onClick={() => onOpenChange(!open)}
        className={cn(
          "flex h-11 shrink-0 items-center gap-2.5 px-3 text-left",
          "focus-visible:outline focus-visible:-outline-offset-2 focus-visible:outline-fg-2",
        )}
      >
        <span aria-hidden="true" className="h-1 w-9 rounded-full bg-line-3" />
        <span className="font-mono text-[10px] uppercase tracking-[0.13em] text-fg">{title}</span>
      </button>

      {!open && peek !== undefined && (
        <div className="overflow-hidden px-3 pb-2 text-fg-2">{peek}</div>
      )}

      {open && <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>}
    </div>
  );
}

/**
 * The nav rail, reflowed.
 *
 * `role="tab"` with `aria-selected` rather than styling alone, because the
 * active surface has to be announced — the icon rail's left marker is
 * invisible to a screen reader either way.
 */
export function TabBar({
  items,
  active,
  onSelect,
}: {
  readonly items: readonly NavItem[];
  readonly active: string;
  readonly onSelect: (id: string) => void;
}): ReactElement {
  return (
    <nav role="tablist" className="flex h-14 shrink-0 items-stretch border-t border-line bg-panel">
      {items.map((item) => (
        <button
          key={item.id}
          role="tab"
          type="button"
          aria-selected={item.id === active}
          onClick={() => onSelect(item.id)}
          className={cn(
            "flex flex-1 flex-col items-center justify-center gap-1",
            "focus-visible:outline focus-visible:-outline-offset-2 focus-visible:outline-fg-2",
            item.id === active ? "text-fg" : "text-fg-3",
          )}
        >
          {item.icon}
          <span className="text-[10px]">{item.label}</span>
        </button>
      ))}
    </nav>
  );
}
