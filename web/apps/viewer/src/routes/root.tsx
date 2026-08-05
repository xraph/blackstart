/**
 * The frame every screen shares.
 *
 * Nav collapses from the 44px icon rail to a bottom tab bar below the tablet
 * breakpoint. The advisory chip inside the top bar is not conditional at any
 * width — the boundary it states does not depend on the viewport.
 */

import { createRootRoute, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { NavRail, TabBar, ThemeToggle, TopBar, useTheme, type NavItem } from "@blackstart/ui";

import { useAuth } from "../auth/provider";

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

/**
 * A badge for whoever is signed in.
 *
 * Prefers the recorded name. Registration asks for first and last, so most
 * accounts have one — and initials derived from a name somebody gave are
 * better than initials guessed from the shape of their address.
 *
 * Accounts predating that form, and any created by other means, still have
 * none, so the address remains the fallback rather than the exception.
 */
export function initialsOfSession(
  session: { firstName: string; lastName: string; email: string } | null,
): string {
  if (session === null) return "—";

  const first = session.firstName.trim();
  const last = session.lastName.trim();

  if (first !== "" || last !== "") {
    const letters = [first, last].filter((p) => p !== "").map((p) => p.charAt(0).toUpperCase());

    return letters.join("");
  }

  return initialsOf(session.email);
}

/**
 * A badge from an address, where no name was recorded.
 *
 * The local part is all there is. Dotted and hyphenated forms give two
 * letters; anything else gives one, which is honest — a manufactured second
 * letter would look like a name the platform never recorded.
 */
export function initialsOf(email: string): string {
  const local = email.split("@")[0] ?? "";
  const parts = local.split(/[._-]+/).filter((p) => p.length > 0);

  if (parts.length === 0) return "—";

  // First and last, not first and second: "rex.i.raphael" is a person with a
  // middle initial, and "RI" names nobody.
  const letters = parts.length === 1 ? [parts[0]] : [parts[0], parts[parts.length - 1]];

  return letters.map((p) => (p ?? "").charAt(0).toUpperCase()).join("");
}

function RootLayout(): React.JSX.Element {
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const active = NAV_ITEMS.find((i) => pathname.startsWith(`/${i.id}`))?.id ?? "console";
  const { session, signOut } = useAuth();
  const { theme, setTheme } = useTheme();

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
        initials={initialsOfSession(session)}
        onSignOut={() => void signOut()}
        aside={<ThemeToggle theme={theme} onChange={setTheme} />}
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
