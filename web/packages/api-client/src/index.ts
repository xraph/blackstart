/**
 * The platform client.
 *
 * Everything under `generated/` is written by `forge client generate` from the
 * host's own OpenAPI document and is never edited by hand — see
 * `.forge-client.yaml` for the source and the path filter. This file is the
 * only hand-written part, and it exists so the package has a stable surface
 * that a regeneration cannot silently reshape.
 *
 * The generated client covers `/api/v1` only. The host also serves Authsome
 * under `/identity` and the Forge dashboard under `/_forge`; those are
 * different surfaces with different audiences, and the viewer binds neither.
 */

export * from "./generated";

import { RESTClient } from "./generated/rest";
import type { ClientConfig } from "./generated/types";

/**
 * Where the API lives.
 *
 * Same origin, always. A deployed build is served by the gateway that serves
 * the API; a development build reaches the host through the Vite dev server's
 * proxy. There is deliberately no absolute base: an absolute one would make
 * the API cross-origin, and a cross-origin request does not carry the session
 * cookie without both `credentials: "include"` here and a credentialed CORS
 * surface on the host — two things to configure in order to arrive back where
 * a proxy starts.
 */
export interface ApiOptions {
  readonly timeoutMs?: number;
}

/**
 * Builds a configured client.
 *
 * No credential is passed, and there is no option to pass one. The session is
 * an HttpOnly cookie the platform sets on sign-in, so `fetch` attaches it to
 * these same-origin requests on its own and the page never holds a token it
 * could leak. See `web/apps/viewer/src/auth`.
 */
export function createApiClient(options: ApiOptions = {}): RESTClient {
  const config: ClientConfig = {
    baseURL: "",
    timeout: options.timeoutMs ?? 30_000,
  };

  return new RESTClient(config);
}

/* ── bitemporal queries ───────────────────────────────────────────────────
 *
 * Most read endpoints accept `validAt` and `knownAt`. They are the same two
 * axes the schema package brands and the console carries in its URL, and they
 * are what makes an as-of view possible at all: `knownAt` asks what the system
 * knew at a moment, not what it knows now.
 *
 * The helper below exists so callers stop hand-writing the pair in the wrong
 * order. They are both ISO strings on the wire and interchanging them produces
 * a plausible answer to a different question. */

/** The bitemporal pair as the generated methods take it. */
export interface AsOfParams {
  readonly validAt: string;
  readonly knownAt: string | undefined;
}

/**
 * Normalises a bitemporal reference for a generated call.
 *
 * An absent knowledge time means "whatever the system knows now", which the
 * API expresses by omitting the parameter rather than by sending the current
 * instant — sending one would pin the answer to a clock reading that the
 * caller did not choose.
 */
export function asOf(validTime: string, knowledgeTime?: string): AsOfParams {
  return { validAt: validTime, knownAt: knowledgeTime };
}
