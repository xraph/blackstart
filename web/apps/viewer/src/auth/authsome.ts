/**
 * The identity calls the console makes.
 *
 * Hand-written against the contract rather than taken from `@authsome/ui-core`.
 * It is two POSTs, and the published React layer for that package is not the
 * one its README advertises — adopting an API I would have to discover, to
 * replace sixty lines whose behaviour I have verified against a running host,
 * is the more expensive of the two options.
 *
 * The publishable key is exactly that: publishable. It identifies which app a
 * credential belongs to and grants nothing on its own, which is why it can sit
 * in a client bundle where a secret could not.
 */

const IDENTITY = "/identity/v1";

/** The token the platform issues, and what it is good for. */
export interface Session {
  readonly token: string;
  readonly refreshToken: string;
  /** ISO instant. The console does not currently refresh on it. */
  readonly expiresAt: string;
  readonly email: string;
  readonly userId: string;
}

export interface Credentials {
  readonly email: string;
  readonly password: string;
}

/**
 * What went wrong, in the platform's own words where it has any.
 *
 * `kind` carries Authsome's machine-readable type so a caller can distinguish
 * "wrong password" from "verify your email", which need different things from
 * the reader and should not be collapsed into one message.
 */
export class IdentityError extends Error {
  readonly status: number;
  readonly kind: string;

  constructor(status: number, message: string, kind: string) {
    super(message);
    this.name = "IdentityError";
    this.status = status;
    this.kind = kind;
  }
}

interface AuthsomeSession {
  session_token?: string;
  refresh_token?: string;
  expires_at?: string;
  user?: { id?: string; email?: string };
}

async function post(
  path: string,
  publishableKey: string,
  baseUrl: string,
  body: unknown,
): Promise<Session> {
  const response = await fetch(`${baseUrl}${IDENTITY}${path}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Publishable-Key": publishableKey,
    },
    body: JSON.stringify(body),
  });

  const payload: unknown = await response.json().catch(() => ({}));

  if (!response.ok) {
    const err = payload as { error?: string; type?: string };

    throw new IdentityError(
      response.status,
      err.error ?? `identity returned ${response.status}`,
      err.type ?? "unknown",
    );
  }

  const data = payload as AuthsomeSession;

  if (data.session_token === undefined || data.session_token === "") {
    // A 2xx with no token is not a session. Failing here beats storing an
    // empty credential and discovering it on the next request as a 401.
    throw new IdentityError(response.status, "identity returned no session token", "no_token");
  }

  return {
    token: data.session_token,
    refreshToken: data.refresh_token ?? "",
    expiresAt: data.expires_at ?? "",
    email: data.user?.email ?? "",
    userId: data.user?.id ?? "",
  };
}

export function signIn(
  credentials: Credentials,
  publishableKey: string,
  baseUrl = "",
): Promise<Session> {
  return post("/signin", publishableKey, baseUrl, credentials);
}

/**
 * Registers an operator and returns their session.
 *
 * Sign-up returns a usable session immediately while sign-in refuses an
 * unverified address — so on a fresh deployment the first operator is signed
 * in by registering, and cannot sign in again until the address is verified.
 * That asymmetry is Authsome's, and the login screen says so rather than
 * letting it read as a broken password.
 */
export function signUp(
  credentials: Credentials,
  publishableKey: string,
  baseUrl = "",
): Promise<Session> {
  return post("/signup", publishableKey, baseUrl, credentials);
}
