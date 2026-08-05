/**
 * Surviving an expiry without signing the operator out.
 *
 * A session lapses while the console is open. The conservative answer — drop
 * to the login screen on the first 401 — interrupts someone mid-cascade for a
 * deadline they could not see coming, when the platform will rotate the
 * session for the asking. So the first 401 buys one refresh and one replay,
 * and only a refusal to rotate ends the session.
 *
 * It wraps the client rather than the transport because there is nowhere else
 * to put it: `ClientConfig` has no fetch override and `generated/` is written
 * by `forge client generate` and never edited by hand. Every generated hook
 * takes the client as an argument, so one Proxy here covers all forty-one
 * methods and changes no generated file.
 */

import { UnauthorizedError } from "@blackstart/api-client";

/**
 * The refresh in flight, shared by every caller.
 *
 * Module-scoped rather than per-client on purpose. Authsome rotates refresh
 * tokens and runs replay detection that cascade-revokes the whole token family
 * when it meets a stale one, so twelve panels expiring in the same tick must
 * produce one `/refresh` and not twelve — twelve would revoke the session this
 * is trying to save.
 */
let inFlight: Promise<void> | null = null;

function refreshOnce(refresh: () => Promise<void>): Promise<void> {
  inFlight ??= refresh().finally(() => {
    // Cleared on settle, or the next expiry an hour from now would replay
    // against a refresh that has long since finished.
    inFlight = null;
  });

  return inFlight;
}

/**
 * Wraps a client so an expired session is refreshed rather than surfaced.
 *
 * `onExpired` fires when the platform refuses to rotate — the session is over
 * and the console has to say so. The original `UnauthorizedError` is raised
 * either way, so a caller that wants to render the failure still can.
 */
export function withReauth<T extends object>(
  client: T,
  refresh: () => Promise<void>,
  onExpired: () => void,
): T {
  return new Proxy(client, {
    get(target, property, receiver): unknown {
      const value = Reflect.get(target, property, receiver) as unknown;

      if (typeof value !== "function") return value;

      return async function reauthed(this: unknown, ...args: unknown[]): Promise<unknown> {
        const call = (): Promise<unknown> =>
          (value as (...a: unknown[]) => Promise<unknown>).apply(target, args);

        try {
          return await call();
        } catch (error: unknown) {
          if (!(error instanceof UnauthorizedError)) throw error;

          try {
            await refreshOnce(refresh);
          } catch {
            // The platform will not rotate the session. Raising the original
            // 401 rather than the refresh failure keeps the reported cause the
            // request the reader actually made.
            onExpired();
            throw error;
          }

          // Exactly one replay. A second 401 is a real refusal — the guard is
          // answering about something other than an expiry — and looping on it
          // would hammer the host with a request it has already declined.
          return await call();
        }
      };
    },
  });
}
