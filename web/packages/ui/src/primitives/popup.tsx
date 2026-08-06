/**
 * The overlay every control in this package opens.
 *
 * Owns only what a dropdown, a menu and a listbox genuinely share: open state,
 * Escape, outside pointer-down, and returning focus where it came from. The
 * keyboard model for what is *inside* the panel belongs to whichever primitive
 * opened it, because a listbox and a menu do not navigate the same way.
 *
 * Hand-rolled rather than vendored, for the reason §8 of the console design
 * gave about auth components: this chrome is achromatic, 9–11px and monospace,
 * and a pre-styled primitive would import a second visual language into a 36px
 * bar. The keyboard model is the price of that, so it is written test-first.
 *
 * **No portal.** Every consumer anchors somewhere that does not clip: the top
 * bar `<header>` and `SideRail`'s subtitle row both sit outside any
 * `overflow-hidden`. A portal costs a focus and testing complication for
 * robustness nothing here needs yet — a consumer inside a clipping container
 * is when to add one, and this comment is the note that it has not happened.
 */

import { useCallback, useEffect, useRef, useState, type ReactElement, type ReactNode } from "react";

import { cn } from "../cn";

export interface Popup {
  readonly open: boolean;
  /** Opening focuses nothing; closing returns focus to the trigger. */
  readonly setOpen: (next: boolean) => void;
  readonly triggerRef: React.RefObject<HTMLButtonElement | null>;
  readonly panelRef: React.RefObject<HTMLDivElement | null>;
}

export function usePopup(): Popup {
  const [open, setOpenState] = useState(false);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const panelRef = useRef<HTMLDivElement | null>(null);

  const setOpen = useCallback((next: boolean) => {
    setOpenState(next);

    /* Without this the reader is left at the top of the document after
     * Escape, which is the same as losing their place. */
    if (!next) triggerRef.current?.focus();
  }, []);

  useEffect(() => {
    if (!open) return;

    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key === "Escape") setOpen(false);
    };

    const onPointerDown = (event: Event): void => {
      const target = event.target as Node | null;
      if (target === null) return;
      if (panelRef.current?.contains(target) === true) return;

      /* The trigger is outside the panel, so closing on any outside pointer
       * would close here and let the click reopen — a control that never
       * appears to close. It toggles itself instead. */
      if (triggerRef.current?.contains(target) === true) return;

      setOpen(false);
    };

    document.addEventListener("keydown", onKeyDown);
    document.addEventListener("pointerdown", onPointerDown);

    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("pointerdown", onPointerDown);
    };
  }, [open, setOpen]);

  return { open, setOpen, triggerRef, panelRef };
}

export interface PopupPanelProps {
  readonly popup: Popup;
  /**
   * Which edge the panel shares with its trigger.
   *
   * `end` for anything on the right of the top bar, so a wide panel grows
   * inward rather than off the screen.
   */
  readonly align?: "start" | "end";
  readonly role?: string;
  readonly id?: string;
  readonly "aria-label"?: string;
  readonly "aria-activedescendant"?: string;
  readonly tabIndex?: number;
  readonly onKeyDown?: (event: React.KeyboardEvent<HTMLDivElement>) => void;
  readonly className?: string;
  readonly children: ReactNode;
}

/**
 * The floating panel, or nothing at all.
 *
 * Nothing at all is the point: a hidden-but-mounted panel keeps its controls
 * focusable, which is a keyboard trap with no visible target. `BottomSheet`
 * unmounts its body for the same reason.
 *
 * A long list scrolls rather than flipping above the trigger. Scrolling is
 * correct at every window height, where a flip only moves the problem, and it
 * needs no measurement pass.
 */
export function PopupPanel({
  popup,
  align = "start",
  className,
  children,
  ...rest
}: PopupPanelProps): ReactElement | null {
  if (!popup.open) return null;

  return (
    <div
      ref={popup.panelRef}
      className={cn(
        "absolute top-full z-50 mt-px max-h-[min(60vh,320px)] overflow-y-auto",
        "rounded-[3px] border border-line-2 bg-panel py-0.5 shadow-lg shadow-black/25",
        align === "end" ? "right-0" : "left-0",
        className,
      )}
      {...rest}
    >
      {children}
    </div>
  );
}
