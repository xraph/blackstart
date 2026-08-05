import type { ReactNode } from "react";

import { cn } from "./cn";

interface PanelProps {
  title: string;
  children: ReactNode;
  className?: string | undefined;
}

/**
 * A floating card over the canvas.
 *
 * The chrome is deliberately separate from the scene: everything here is plain
 * DOM sitting above the WebGL canvas, so nothing about the inspector can cost
 * a frame. Rendering panel text into the 3D scene would make every keystroke a
 * geometry update.
 */
export function Panel({ title, children, className }: PanelProps): React.JSX.Element {
  return (
    <section
      className={cn(
        "pointer-events-auto rounded-[4px] border border-line-2",
        "bg-[var(--bs-glass)] px-4 py-3 text-sm text-fg shadow-xl backdrop-blur",
        className,
      )}
    >
      <h2 className="mb-2 font-mono text-[9.5px] uppercase tracking-[0.13em] text-fg-3">{title}</h2>
      {children}
    </section>
  );
}

interface FieldProps {
  label: string;
  value: ReactNode;
  /** Renders the value in a monospace face, for identifiers and hashes. */
  mono?: boolean | undefined;
}

/** One labelled value inside a panel. */
export function Field({ label, value, mono }: FieldProps): React.JSX.Element {
  return (
    <div className="flex items-baseline justify-between gap-4 py-0.5">
      <span className="shrink-0 text-fg-3">{label}</span>
      <span className={cn("truncate text-right", mono && "font-mono text-xs tabular")}>
        {value}
      </span>
    </div>
  );
}
