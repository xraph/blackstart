/**
 * Whoever is signed in, and the one action attached to them.
 *
 * The badge used to *be* the sign-out button. That made a session-ending
 * action reachable by one press of an unlabelled 20px target — announceable,
 * because it carried an aria-label, but not deliberate. The two are different
 * properties and only the second protects anybody.
 *
 * The metrics are unchanged either way. The bar is 36px tall and a badge that
 * changed size when a menu appeared behind it would move the clock.
 */

import type { ReactElement } from "react";

import { Menu } from "../primitives/menu";

const BADGE =
  "flex size-5 items-center justify-center rounded-[3px] border border-line-2 bg-panel-3 text-[9px] font-semibold text-fg-2";

export interface AccountSession {
  readonly userId: string;
  readonly email: string;
  /** Empty where the account has none; the address stands in. */
  readonly name: string;
}

export function AccountMenu({
  initials,
  session,
  onSignOut,
}: {
  readonly initials: string;
  /** Null before sign-in resolves, when the badge is text and nothing more. */
  readonly session: AccountSession | null;
  readonly onSignOut: () => void;
}): ReactElement {
  if (session === null) return <span className={BADGE}>{initials}</span>;

  return (
    <Menu
      label="Account"
      triggerClassName="rounded-[3px]"
      trigger={<span className={`${BADGE} hover:border-line-3 hover:text-fg`}>{initials}</span>}
      header={
        <>
          {session.name !== "" && <p className="m-0 text-[11px] text-fg">{session.name}</p>}
          <p className="m-0 text-[10.5px] text-fg-2">{session.email}</p>
          {/* Selectable text rather than a copy button: this is the field a
              support conversation asks for, and selecting needs no clipboard
              permission and no confirmation the copy happened. */}
          <p className="m-0 mt-1 select-text font-mono text-[9.5px] tabular text-fg-3">
            {session.userId}
          </p>
        </>
      }
      items={[{ id: "sign-out", label: "Sign out", onSelect: onSignOut, tone: "strong" }]}
    />
  );
}
