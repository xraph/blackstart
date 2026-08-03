/**
 * The layer stack.
 *
 * Each row carries the version of the data it is drawing, inline. That is what
 * makes a screenshot self-describing — a figure in a review can be traced
 * without asking who took it and when.
 */

import type { ReactElement } from "react";
import { activeLayerCount, type Layer, type LayerStack as Stack } from "@blackstart/schema";
import { cn } from "../cn";

export function LayerStackPanel({
  layers,
  onToggle,
  onOpacity,
}: {
  readonly layers: Stack;
  readonly onToggle?: (id: string) => void;
  readonly onOpacity?: (id: string, value: number) => void;
}): ReactElement {
  return (
    <div>
      {layers.map((l) => (
        <LayerRow key={l.id} layer={l} onToggle={onToggle} onOpacity={onOpacity} />
      ))}
    </div>
  );
}

/** `4 / 7`, for the panel header. */
export function layerSummary(layers: Stack): string {
  return `${activeLayerCount(layers)} / ${layers.length}`;
}

function LayerRow({
  layer,
  onToggle,
  onOpacity,
}: {
  readonly layer: Layer;
  /* Explicitly `| undefined`: under exactOptionalPropertyTypes an omitted
   * prop and one passed as undefined are different, and these arrive from a
   * parent that may hold neither handler. */
  readonly onToggle?: ((id: string) => void) | undefined;
  readonly onOpacity?: ((id: string, value: number) => void) | undefined;
}): ReactElement {
  return (
    <div className="flex h-[30px] items-center gap-2 border-b border-line-2 px-[9px] last:border-b-0">
      <button
        type="button"
        role="checkbox"
        aria-checked={layer.state === "on" ? "true" : layer.state === "partial" ? "mixed" : "false"}
        aria-label={layer.label}
        onClick={() => onToggle?.(layer.id)}
        className={cn(
          "touch-target flex size-[11px] shrink-0 items-center justify-center rounded-[2px] border",
          "focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2",
          layer.state === "on"
            ? "border-fg bg-fg"
            : layer.state === "partial"
              ? "border-line-3 bg-fg-4"
              : "border-line-3",
        )}
      >
        {layer.state === "on" && (
          <svg viewBox="0 0 10 10" className="size-2 stroke-ink" strokeWidth="2" fill="none">
            <path d="M2 5.2l2.2 2.2L8 3" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        )}
      </button>

      <span className="w-[74px] shrink-0 text-[11px] text-fg">{layer.label}</span>

      <label className="sr-only" htmlFor={`opacity-${layer.id}`}>
        {layer.label} opacity
      </label>
      <input
        id={`opacity-${layer.id}`}
        type="range"
        min={0}
        max={1}
        step={0.01}
        value={layer.opacity}
        onChange={(e) => onOpacity?.(layer.id, Number(e.target.value))}
        className="h-0.5 w-[38px] shrink-0 appearance-none rounded-[1px] bg-line-2 accent-[var(--bs-fg)]"
      />

      <span className="flex-1 text-right font-mono text-[9.5px] tabular text-fg-3">
        {layer.source}
      </span>
    </div>
  );
}
