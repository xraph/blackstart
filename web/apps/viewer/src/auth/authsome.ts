/**
 * The identity calls the console makes.
 *
 * Hand-written against the contract rather than taken from `@authsome/ui-core`.
 * It is five calls, and the published React layer for that package is not the
 * one its README advertises — adopting an API I would have to discover, to
 * replace this, is the more expensive of the two options.
 *
 * The session is a cookie. The platform sets it `HttpOnly` on sign-in, deletes
 * it on sign-out, and rotates it cookie-first on refresh, so nothing here ever
 * sees the credential and nothing here can store one. `fetch` attaches it
 * because every one of these is same-origin — in a deployed build because the
 * gateway serves both, in development because the Vite dev server proxies
 * `/identity` to the host.
 *
 * The publishable key is exactly that: publishable. It identifies which app a
 * credential belongs to and grants nothing on its own, which is why it can sit
 * in a client bundle where a secret could not.
 */

const IDENTITY = "/identity/v1";

/**
 * Who is signed in.
 *
 * An identity, not a credential. There is deliberately no field for a token:
 * the cookie is HttpOnly, so a token here could only be a second copy of
 * something this page is not supposed to be able to read.
 */
export interface Session {
  readonly userId: string;
  readonly email: string;

  /**
   * The operator's name, empty where the account has none.
   *
   * Carried because the console attributes things to people — a run, a
   * dismissed contingency — and an email prefix is a poor name to put in a
   * report somebody else reads. Empty rather than absent: Authsome always
   * serialises both fields, and an account registered before the form asked
   * for them simply has them blank.
   */
  readonly firstName: string;
  readonly lastName: string;
}

/**
 * What to call the operator, in one string.
 *
 * Falls back to the email rather than rendering an empty badge or the word
 * "Unknown": the address is the one thing every account has, and it is what
 * they typed to get here.
 */
export function displayName(session: Session): string {
  const full = [session.firstName, session.lastName].filter((p) => p !== "").join(" ");

  return full === "" ? session.email : full;
}

export interface Credentials {
  readonly email: string;
  readonly password: string;
}

/**
 * What registering an operator carries beyond a credential.
 *
 * first_name and last_name are first-class on Authsome's signup body, not
 * custom fields — so they go on the wire under those names rather than into
 * `metadata`. Metadata is validated against an app's active signup FormConfig,
 * and this deployment publishes none; a name sent that way would be stored
 * unvalidated under a key nothing else reads.
 *
 * Both optional at the contract, and optional here for the same reason: a
 * deployment that has not asked for names should not be made to invent them.
 * The screen requires them because a console where every run is attributed to
 * an email prefix is worse to operate, which is a product decision and belongs
 * there rather than in this file.
 */
export interface Registration extends Credentials {
  readonly firstName?: string;
  readonly lastName?: string;
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

/** A user as the platform serialises one, wherever it appears. */
interface WireUser {
  id?: string;
  email?: string;
  first_name?: string;
  last_name?: string;
}

/** Sign-in and sign-up wrap the user; `/me` returns one bare. */
interface WireAuth {
  user?: WireUser;
}

async function bodyOf(response: Response): Promise<unknown> {
  return response.json().catch(() => ({}));
}

/** The platform's own refusal, or a generic one when it offered none. */
function refusal(status: number, payload: unknown): IdentityError {
  const err = payload as { error?: string; type?: string };

  return new IdentityError(
    status,
    err.error ?? `identity returned ${status}`,
    err.type ?? "unknown",
  );
}

/**
 * A user, or a refusal to accept a response that names none.
 *
 * A 2xx with no user is not a session. Failing here beats reporting an
 * operator signed in as nobody and discovering it three screens later as a
 * 401 that points at nothing.
 */
function sessionOf(status: number, user: WireUser | undefined): Session {
  if (user?.id === undefined || user.id === "") {
    throw new IdentityError(status, "identity returned no user", "no_user");
  }

  return {
    userId: user.id,
    email: user.email ?? "",
    firstName: user.first_name ?? "",
    lastName: user.last_name ?? "",
  };
}

async function authenticate(
  path: string,
  publishableKey: string,
  body: Record<string, string>,
): Promise<Session> {
  const response = await fetch(`${IDENTITY}${path}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Publishable-Key": publishableKey,
    },
    body: JSON.stringify(body),
  });

  const payload = await bodyOf(response);

  if (!response.ok) throw refusal(response.status, payload);

  return sessionOf(response.status, (payload as WireAuth).user);
}

export function signIn(credentials: Credentials, publishableKey: string): Promise<Session> {
  return authenticate("/signin", publishableKey, {
    email: credentials.email,
    password: credentials.password,
  });
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
export function signUp(registration: Registration, publishableKey: string): Promise<Session> {
  const body: Record<string, string> = {
    email: registration.email,
    password: registration.password,
  };

  // Omitted rather than sent empty. Both fields are `omitempty` on Authsome's
  // side, and a blank string is a name the account then has — it would show in
  // the identity badge as whitespace rather than falling back to the email.
  const first = registration.firstName?.trim() ?? "";
  const last = registration.lastName?.trim() ?? "";

  if (first !== "") body["first_name"] = first;
  if (last !== "") body["last_name"] = last;

  return authenticate("/signup", publishableKey, body);
}

/**
 * Who the cookie says is signed in, or null.
 *
 * A 401 is the answer to the question rather than a failure to answer it, so
 * it returns null. Throwing would put an error panel in front of a first-time
 * visitor whose only mistake was not having signed in yet. Anything else is a
 * genuine fault and is raised.
 *
 * Note the shape: `/me` returns a user at the top level, where sign-in wraps
 * one under `user`.
 */
export async function me(): Promise<Session | null> {
  const response = await fetch(`${IDENTITY}/me`, { method: "GET" });

  if (response.status === 401) return null;

  const payload = await bodyOf(response);

  if (!response.ok) throw refusal(response.status, payload);

  return sessionOf(response.status, payload as WireUser);
}

/**
 * Ends the session on the host.
 *
 * Clearing local state alone would leave a live session the next page load
 * silently resumes. A 401 is not raised: the operator asked to be signed out,
 * and a session that is already gone is not a failure to report to them.
 */
export async function signOut(): Promise<void> {
  const response = await fetch(`${IDENTITY}/signout`, { method: "POST" });

  if (!response.ok && response.status !== 401) {
    throw refusal(response.status, await bodyOf(response));
  }
}

/**
 * Rotates the session.
 *
 * An empty object, not an empty body, and the difference is the whole call.
 * The platform binds its request struct before the handler runs, so a bodyless
 * POST fails at `failed to decode JSON: EOF` with a 400 and never reaches the
 * cookie-first branch that makes this work at all. Verified against a running
 * host, having first been written the other way.
 *
 * The object stays empty because the page holds no refresh token to put in it
 * — the cookie is the credential, and rotating it is the platform's job.
 */
export async function refresh(): Promise<void> {
  const response = await fetch(`${IDENTITY}/refresh`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
  });

  if (!response.ok) throw refusal(response.status, await bodyOf(response));
}
