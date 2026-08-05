/**
 * The frame every screen shares.
 *
 * Slots rather than routes: the shell knows there is a stage and a rail, not
 * what is in them. That is what lets the cascade screen swap the time axis for
 * a transport without the shell growing a mode flag.
 */

import type { ReactElement, ReactNode } from "react";
import { cn } from "../cn";
import { AdvisoryChip } from "../instrument/status";

export interface AppShellProps {
  readonly topBar: ReactNode;
  readonly nav: ReactNode;
  readonly stage: ReactNode;
  readonly rail?: ReactNode;
  readonly footer?: ReactNode;
  /**
   * Pinned to a past knowledge time. Outlines the whole frame, because a
   * number computed from a deliberately stale knowledge state looks exactly
   * like a live one otherwise.
   */
  readonly asOf?: boolean;
  readonly className?: string;
}

export function AppShell({
  topBar,
  nav,
  stage,
  rail,
  footer,
  asOf = false,
  className,
}: AppShellProps): ReactElement {
  return (
    <div
      className={cn(
        "flex h-full flex-col overflow-hidden bg-bg text-xs text-fg",
        asOf && "outline outline-1 -outline-offset-1 outline-amber",
        className,
      )}
      data-as-of={asOf ? "true" : undefined}
    >
      {topBar}
      <div className="flex min-h-0 flex-1">
        {nav}
        <div className="relative min-w-0 flex-1 overflow-hidden bg-stage">{stage}</div>
        {rail}
      </div>
      {footer}
    </div>
  );
}

/* ── top bar ─────────────────────────────────────────────────────────── */

export interface TopBarContext {
  readonly label: string;
  readonly value: string;
  readonly menu?: boolean;
}

export function TopBar({
  contexts,
  clock,
  initials,
  onSignOut,
}: {
  readonly contexts: readonly TopBarContext[];
  readonly clock: string;
  readonly initials: string;
  /** Turns the identity badge into a sign-out control. Omitted, it stays text. */
  readonly onSignOut?: () => void;
}): ReactElement {
  /* The badge, with or without an action on it. Same box either way — the bar
   * is 36px tall and a button that changed its metrics would move the clock. */
  const badge =
    "flex size-5 items-center justify-center rounded-[3px] border text-[9px] font-semibold";
  return (
    <header className="flex h-9 shrink-0 items-center border-b border-line bg-panel pl-3">
      <span className="pr-4 font-mono text-[11.5px] font-bold tracking-[0.16em] text-fg">
        BLACKSTART
      </span>
      {contexts.map((c) => (
        <div key={c.label} className="flex h-full items-center">
          <span aria-hidden="true" className="h-[18px] w-px bg-line-2" />
          <span className="flex h-full items-center gap-2 px-3">
            <span className="text-[10px] uppercase tracking-[0.1em] text-fg-3">{c.label}</span>
            <span className="font-mono text-[11.5px] text-fg">{c.value}</span>
            {c.menu === true && <span className="text-[9px] text-fg-3">▾</span>}
          </span>
        </div>
      ))}
      <span className="flex-1" />
      <AdvisoryChip className="mx-2.5" />
      <span className="flex h-full items-center border-l border-line px-3 font-mono text-[11px] tabular text-fg-2">
        {clock}
      </span>
      <span className="flex h-full items-center border-l border-line px-3">
        {onSignOut === undefined ? (
          <span className={`${badge} border-line-2 bg-panel-3 text-fg-2`}>{initials}</span>
        ) : (
          /* Labelled rather than left to the initials: "RR" tells a screen
           * reader nothing about what pressing it does. */
          <button
            type="button"
            onClick={onSignOut}
            aria-label={`Sign out ${initials}`}
            title="Sign out"
            className={`${badge} border-line-2 bg-panel-3 text-fg-2 hover:border-line-3 hover:text-fg focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2`}
          >
            {initials}
          </button>
        )}
      </span>
    </header>
  );
}

/* ── nav rail ────────────────────────────────────────────────────────── */

export interface NavItem {
  readonly id: string;
  readonly label: string;
  readonly icon: ReactNode;
  /** Pushed to the bottom of the rail. */
  readonly footer?: boolean;
}

export function NavRail({
  items,
  active,
  onSelect,
}: {
  readonly items: readonly NavItem[];
  readonly active: string;
  readonly onSelect?: (id: string) => void;
}): ReactElement {
  const render = (item: NavItem) => {
    const on = item.id === active;
    return (
      <button
        key={item.id}
        type="button"
        title={item.label}
        aria-label={item.label}
        aria-current={on ? "page" : undefined}
        onClick={() => onSelect?.(item.id)}
        className={cn(
          "relative flex size-8 items-center justify-center rounded-[3px]",
          "focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2",
          on ? "bg-panel-3 text-fg" : "text-fg-3 hover:text-fg-2",
        )}
      >
        {on && (
          <span
            aria-hidden="true"
            className="absolute inset-y-1.5 -left-1.5 w-0.5 rounded-[1px] bg-fg"
          />
        )}
        {item.icon}
      </button>
    );
  };

  return (
    <nav className="flex w-11 shrink-0 flex-col items-center gap-0.5 border-r border-line bg-panel pt-1.5">
      {items.filter((i) => i.footer !== true).map(render)}
      <span className="flex-1" />
      {items.filter((i) => i.footer === true).map(render)}
    </nav>
  );
}

/* ── side rail ───────────────────────────────────────────────────────── */

export function SideRail({
  title,
  count,
  subtitle,
  width = 320,
  children,
}: {
  readonly title: string;
  readonly count?: string;
  readonly subtitle?: ReactNode;
  readonly width?: number;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <aside
      className="flex min-h-0 shrink-0 flex-col border-l border-line bg-panel"
      style={{ width }}
    >
      <div className="flex h-[34px] shrink-0 items-center gap-2 border-b border-line px-[11px]">
        <h2 className="m-0 font-mono text-[10px] uppercase tracking-[0.13em] text-fg">{title}</h2>
        <span className="flex-1" />
        {count !== undefined && (
          <span className="font-mono text-[9.5px] tabular text-fg-3">{count}</span>
        )}
      </div>
      {subtitle !== undefined && (
        <div className="flex h-6 shrink-0 items-center gap-1.5 border-b border-line bg-panel-2 px-[11px] font-mono text-[9px] tracking-[0.08em] text-fg-3">
          {subtitle}
        </div>
      )}
      <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
    </aside>
  );
}

/**
 * A panel that floats over the globe.
 *
 * Anchored to a corner rather than positioned ad hoc, because chrome scattered
 * across a map competes with the map.
 */
export function GlassPanel({
  title,
  meta,
  className,
  style,
  children,
}: {
  readonly title?: string;
  readonly meta?: string;
  readonly className?: string;
  readonly style?: React.CSSProperties;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <div
      className={cn("absolute rounded-[4px] border border-line-2 backdrop-blur-[6px]", className)}
      style={{ background: "var(--bs-glass)", ...style }}
    >
      {title !== undefined && (
        <div className="flex h-[26px] items-center gap-[7px] border-b border-line px-[9px] font-mono text-[9.5px] uppercase tracking-[0.13em] text-fg-3">
          {title}
          <span className="flex-1" />
          {meta !== undefined && <span className="tracking-normal">{meta}</span>}
        </div>
      )}
      {children}
    </div>
  );
}
