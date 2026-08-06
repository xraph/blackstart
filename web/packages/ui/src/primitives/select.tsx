/**
 * Choosing one of a few.
 *
 * The listbox pattern rather than the menu one: these options are a *value*,
 * and the trigger goes on showing which. Focus stays on the trigger and the
 * active option is named by `aria-activedescendant`, so the reader's caret
 * never leaves the control they opened — which is also what lets typeahead
 * work without a hidden input to type into.
 *
 * `<select>` was the other candidate and loses on two counts: it cannot carry
 * the monospace meta column these options need — a model's content hash, a
 * scenario's date — and its popup is drawn by the platform, in a visual
 * language that is not this one.
 */

import { useId, useMemo, useRef, useState, type ReactElement } from "react";

import { cn } from "../cn";
import { PopupPanel, usePopup } from "./popup";

export interface SelectOption<T extends string> {
  readonly value: T;
  readonly label: string;
  /** Right-aligned monospace detail: a content hash, a date, a count. */
  readonly meta?: string;
}

export interface SelectProps<T extends string> {
  /** Names the control for assistive technology. Not rendered by the trigger. */
  readonly label: string;
  /** Absent means nothing is chosen, which `placeholder` states. */
  readonly value: T | undefined;
  readonly options: readonly SelectOption<T>[];
  readonly onChange: (value: T) => void;
  readonly placeholder?: string;
  readonly align?: "start" | "end";
  readonly className?: string;
  readonly triggerClassName?: string;
}

/** How long consecutive keystrokes count as one typeahead query. */
const TYPEAHEAD_MS = 800;

export function Select<T extends string>({
  label,
  value,
  options,
  onChange,
  placeholder = "—",
  align = "start",
  className,
  triggerClassName,
}: SelectProps<T>): ReactElement {
  const popup = usePopup();
  const listId = useId();
  const optionId = (index: number): string => `${listId}-${index}`;

  const selected = useMemo(
    () => options.findIndex((option) => option.value === value),
    [options, value],
  );

  const [activeIndex, setActiveIndex] = useState(0);

  /* Typeahead state is a ref, not state: it is read during a keystroke and
   * never rendered, and re-rendering the panel on every letter would fight
   * the very list the reader is trying to reach. */
  const typed = useRef<{ query: string; at: number }>({ query: "", at: 0 });

  const empty = options.length === 0;

  const show = (index: number): void => {
    setActiveIndex(index);
    popup.setOpen(true);
  };

  const commit = (index: number): void => {
    const option = options[index];
    if (option !== undefined) onChange(option.value);
    popup.setOpen(false);
  };

  const seek = (key: string, from: number): number => {
    const now = Date.now();
    const query = (now - typed.current.at < TYPEAHEAD_MS ? typed.current.query : "") + key;
    typed.current = { query, at: now };

    /* Search from the one after the active option so repeating a letter
     * cycles through the options that share it rather than sticking. */
    const start = query.length === 1 ? from + 1 : from;

    for (let step = 0; step < options.length; step += 1) {
      const index = (start + step + options.length) % options.length;
      if (options[index]?.label.toLowerCase().startsWith(query.toLowerCase()) === true)
        return index;
    }

    return from;
  };

  const onKeyDown = (event: React.KeyboardEvent<HTMLButtonElement>): void => {
    if (empty) return;

    const last = options.length - 1;
    const from = popup.open ? activeIndex : Math.max(selected, 0);

    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        show(popup.open ? (from === last ? 0 : from + 1) : from);
        return;

      case "ArrowUp":
        event.preventDefault();
        show(popup.open ? (from === 0 ? last : from - 1) : from);
        return;

      case "Home":
        if (!popup.open) return;
        event.preventDefault();
        setActiveIndex(0);
        return;

      case "End":
        if (!popup.open) return;
        event.preventDefault();
        setActiveIndex(last);
        return;

      case "Enter":
      case " ":
        if (!popup.open) return;
        event.preventDefault();
        commit(activeIndex);
        return;

      case "Tab":
        /* Closes without committing. Tabbing away is leaving, not choosing —
         * a listbox that commits on Tab changes a value the reader never
         * looked at. */
        if (popup.open) popup.setOpen(false);
        return;

      default:
        break;
    }

    if (event.key.length === 1 && !event.altKey && !event.ctrlKey && !event.metaKey) {
      event.preventDefault();
      show(seek(event.key, from));
    }
  };

  const current = selected >= 0 ? options[selected] : undefined;

  return (
    <div className={cn("relative", className)}>
      <button
        ref={popup.triggerRef}
        type="button"
        role="combobox"
        aria-label={label}
        aria-expanded={popup.open}
        aria-haspopup="listbox"
        aria-controls={popup.open ? listId : undefined}
        aria-activedescendant={popup.open ? optionId(activeIndex) : undefined}
        disabled={empty}
        onClick={() => (popup.open ? popup.setOpen(false) : show(Math.max(selected, 0)))}
        onKeyDown={onKeyDown}
        className={cn(
          "flex items-center gap-1.5 rounded-[3px] px-1.5 py-0.5 text-left",
          "focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2",
          empty ? "cursor-default text-fg-3" : "hover:bg-hl",
          triggerClassName,
        )}
      >
        <span
          className={cn("font-mono text-[11.5px]", current === undefined ? "text-fg-3" : "text-fg")}
        >
          {current?.label ?? placeholder}
        </span>
        <span aria-hidden="true" className="text-[9px] text-fg-3">
          ▾
        </span>
      </button>

      <PopupPanel
        popup={popup}
        align={align}
        id={listId}
        role="listbox"
        aria-label={label}
        className="min-w-full"
      >
        {options.map((option, index) => (
          <div
            key={option.value}
            id={optionId(index)}
            role="option"
            aria-selected={option.value === value}
            onClick={() => commit(index)}
            onMouseEnter={() => setActiveIndex(index)}
            className={cn(
              "flex cursor-default items-center gap-3 whitespace-nowrap px-2 py-1",
              index === activeIndex ? "bg-panel-3" : "",
            )}
          >
            <span
              aria-hidden="true"
              className={cn(
                "w-2 shrink-0 text-[10px]",
                option.value === value ? "text-fg" : "text-transparent",
              )}
            >
              ✓
            </span>
            <span className="flex-1 font-mono text-[11.5px] text-fg">{option.label}</span>
            {option.meta !== undefined && (
              <span className="shrink-0 font-mono text-[9.5px] tabular text-fg-3">
                {option.meta}
              </span>
            )}
          </div>
        ))}
      </PopupPanel>
    </div>
  );
}
