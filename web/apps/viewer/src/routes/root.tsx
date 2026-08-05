/**
 * The frame every screen shares.
 *
 * Nav collapses from the 44px icon rail to a bottom tab bar below the tablet
 * breakpoint. The advisory chip inside the top bar is not conditional at any
 * width — the boundary it states does not depend on the viewport.
 */

import { createRootRoute, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { NavRail, TabBar, TopBar, type NavItem } from "@blackstart/ui";

/* Inline SVG rather than an icon dependency: four of them do not justify
 * shipping a set, and these are drawn at the shell's own stroke weight. */
const icon = (paths: React.ReactNode): React.ReactElement => (
  <svg
    viewBox="0 0 24 24"
    className="size-4 fill-none stroke-current stroke-[1.4]"
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
  >
    {paths}
  </svg>
);

export const NAV_ITEMS: NavItem[] = [
  {
    id: "console",
    label: "Console",
    icon: icon(
      <>
        <circle cx="12" cy="12" r="9" />
        <path d="M3 12h18M12 3c2.5 2.6 3.8 5.7 3.8 9s-1.3 6.4-3.8 9c-2.5-2.6-3.8-5.7-3.8-9S9.5 5.6 12 3z" />
      </>,
    ),
  },
  {
    id: "cascade",
    label: "Cascade",
    icon: icon(
      <>
        <path d="M4 5h5v5H4zM15 14h5v5h-5zM6.5 10v4h11" />
        <path d="M15 8l2.5-2.5L20 8" />
      </>,
    ),
  },
  {
    id: "runs",
    label: "Runs",
    icon: icon(
      <>
        <rect x="3.5" y="4.5" width="17" height="15" rx="1.5" />
        <path d="M3.5 9.5h17M9 9.5v10M3.5 14.5h17" />
      </>,
    ),
  },
];

function RootLayout(): React.JSX.Element {
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const active = NAV_ITEMS.find((i) => pathname.startsWith(`/${i.id}`))?.id ?? "console";

  /* Navigating without search drops the current params, which would lose the
   * valid time. The console and cascade screens share a schema, so carrying
   * them across is correct; the runs screen validates its own and ignores
   * what it does not recognise. */
  const go = (id: string): void => {
    void navigate({ to: `/${id}`, search: (prev: Record<string, unknown>) => prev });
  };

  return (
    <div className="flex h-full flex-col bg-bg text-fg">
      <TopBar
        contexts={[
          { label: "scenario", value: "TX-2026-0802-DERECHO", menu: true },
          { label: "case", value: "ACTIVSg2000", menu: true },
        ]}
        clock="18:14:07 Z"
        initials="RR"
      />

      <div className="flex min-h-0 flex-1">
        <div className="hidden md:flex">
          <NavRail items={NAV_ITEMS} active={active} onSelect={go} />
        </div>
        <div className="relative min-w-0 flex-1">
          <Outlet />
        </div>
      </div>

      <div className="md:hidden">
        <TabBar items={NAV_ITEMS} active={active} onSelect={go} />
      </div>
    </div>
  );
}

export const Route = createRootRoute({ component: RootLayout });
