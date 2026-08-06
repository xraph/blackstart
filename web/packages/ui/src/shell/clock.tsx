/**
 * What time it is, and what time the console is reasoning from.
 *
 * The bar carried `18:14:07 Z` as a literal. On a forecast console a stopped
 * clock is not an unfinished control — it states a time, and it is wrong one
 * second after the page loads.
 *
 * It is labelled `NOW` because this console keeps valid time and knowledge
 * time apart everywhere else, and an unlabelled wall clock beside them is a
 * third quantity nobody asked which it was.
 */

import { useEffect, useState, type ReactElement } from "react";

import { cn } from "../cn";

/** `18:14:07`, always eight characters, from an instant. */
function hms(at: Date): string {
  const pad = (n: number): string => String(n).padStart(2, "0");

  return `${pad(at.getUTCHours())}:${pad(at.getUTCMinutes())}:${pad(at.getUTCSeconds())}`;
}

/**
 * The wall clock, ticking.
 *
 * Re-aligned to the wall second on every tick rather than left on a plain
 * 1000ms interval: an interval drifts, and a background tab that throttles it
 * comes back showing a time that is merely close. A clock that is close is a
 * clock that is wrong.
 */
export function useUtcClock(): Date {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>;

    const tick = (): void => {
      const at = new Date();
      setNow(at);
      timer = setTimeout(tick, 1000 - at.getUTCMilliseconds());
    };

    tick();

    return () => clearTimeout(timer);
  }, []);

  return now;
}

export function Clock({
  /**
   * The knowledge time the console is pinned to, or absent when live.
   *
   * Absence *is* the live case, matching the URL contract — encoding it as a
   * sentinel would put a second spelling of "live" in the console.
   */
  knownAt,
  className,
}: {
  readonly knownAt?: string;
  readonly className?: string;
}): ReactElement {
  const now = useUtcClock();
  const pinned = knownAt === undefined ? null : new Date(knownAt);

  return (
    <span className={cn("flex items-center gap-2.5", className)}>
      {/* Hidden below the tablet breakpoint, where the as-of segment keeps
          the room. A reader who cannot see the wall clock has a watch; one
          who cannot see that they are in a backtest has nothing. */}
      <span className="flex items-center gap-1.5 max-md:hidden">
        <span className="text-[9px] uppercase tracking-[0.1em] text-fg-3">NOW</span>
        <span className="font-mono text-[11px] tabular text-fg-2">{hms(now)} Z</span>
      </span>

      {pinned !== null && (
        <span
          className="flex items-center gap-1.5 rounded-[3px] border border-amber-line bg-amber-bg px-1.5 py-px"
          title="The console is pinned to a past knowledge time"
        >
          <span className="text-[9px] uppercase tracking-[0.1em] text-amber">AS OF</span>
          {/* To the minute, not the second. A knowledge time is chosen by
              a reader dragging a marker; seconds imply a precision the
              choice never had. */}
          <span className="font-mono text-[11px] tabular text-amber">
            {hms(pinned).slice(0, 5)} Z
          </span>
        </span>
      )}
    </span>
  );
}
