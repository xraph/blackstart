/**
 * A short list of actions hung off a control.
 *
 * Separate from `Select` rather than one component with a `role` prop, because
 * the two genuinely differ in where the reader's caret goes. A listbox keeps
 * focus on the trigger and points at the active option, since the option is a
 * value the trigger goes on showing. A menu moves focus onto the item, since
 * the item is a thing about to happen and the trigger will show nothing after.
 *
 * A menu also carries header content — an identity block, a session detail —
 * which a listbox has nowhere to put.
 */

import { useEffect, useRef, type ReactElement, type ReactNode } from "react";

import { cn } from "../cn";
import { PopupPanel, usePopup } from "./popup";

export interface MenuItem {
  readonly id: string;
  readonly label: string;
  readonly onSelect: () => void;
  /**
   * Draws the item as consequential. Sign-out is the only one today.
   *
   * A tone rather than a `destructive` boolean: the chrome is achromatic by
   * §2 of the console design, so this changes weight and border, not hue.
   */
  readonly tone?: "default" | "strong";
}

export interface MenuProps {
  /** Names the trigger. `RR` tells a screen reader nothing about what it does. */
  readonly label: string;
  readonly trigger: ReactNode;
  /** Read, not activated. Rendered above the items and outside the item list. */
  readonly header?: ReactNode;
  readonly items: readonly MenuItem[];
  readonly align?: "start" | "end";
  readonly className?: string;
  readonly triggerClassName?: string;
}

export function Menu({
  label,
  trigger,
  header,
  items,
  align = "end",
  className,
  triggerClassName,
}: MenuProps): ReactElement {
  const popup = usePopup();
  const listRef = useRef<HTMLDivElement | null>(null);

  const focusables = (): HTMLElement[] =>
    Array.from(listRef.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? []);

  /* Focus lands on the first item once the panel exists, which is after the
   * render that opened it — hence an effect rather than a call inside the
   * click handler, where there is nothing yet to focus. */
  useEffect(() => {
    if (!popup.open) return;
    focusables()[0]?.focus();
  }, [popup.open]);

  const roam = (event: React.KeyboardEvent<HTMLDivElement>, step: number): void => {
    const all = focusables();
    const here = all.indexOf(event.target as HTMLElement);
    if (here < 0 || all.length === 0) return;

    event.preventDefault();
    all[(here + step + all.length) % all.length]?.focus();
  };

  const onKeyDown = (event: React.KeyboardEvent<HTMLDivElement>): void => {
    if (event.key === "ArrowDown") roam(event, 1);
    else if (event.key === "ArrowUp") roam(event, -1);
    else if (event.key === "Home" || event.key === "End") {
      const all = focusables();
      event.preventDefault();
      (event.key === "Home" ? all[0] : all[all.length - 1])?.focus();
    }
  };

  return (
    <div className={cn("relative", className)}>
      <button
        ref={popup.triggerRef}
        type="button"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={popup.open}
        onClick={() => popup.setOpen(!popup.open)}
        className={cn(
          "focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2",
          triggerClassName,
        )}
      >
        {trigger}
      </button>

      <PopupPanel popup={popup} align={align} className="min-w-[184px] py-0">
        <div ref={listRef} role="menu" aria-label={label} onKeyDown={onKeyDown}>
          {header !== undefined && (
            <div className="border-b border-line px-2.5 py-2 leading-snug">{header}</div>
          )}

          <div className="py-0.5">
            {items.map((item) => (
              <button
                key={item.id}
                type="button"
                role="menuitem"
                onClick={() => {
                  popup.setOpen(false);
                  item.onSelect();
                }}
                className={cn(
                  "block w-full px-2.5 py-1 text-left text-[11px] hover:bg-hl",
                  "focus-visible:outline focus-visible:-outline-offset-2 focus-visible:outline-fg-2",
                  item.tone === "strong" ? "text-fg" : "text-fg-2",
                )}
              >
                {item.label}
              </button>
            ))}
          </div>
        </div>
      </PopupPanel>
    </div>
  );
}
