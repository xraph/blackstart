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
 * Same-origin by default, because the client is served by the gateway that
 * serves the API. An absolute URL is for development against a host running
 * somewhere else, and is the only case that needs CORS.
 */
export interface ApiOptions {
  readonly baseUrl?: string;
  readonly timeoutMs?: number;
}

/**
 * Builds a configured client.
 *
 * No credential is passed. Sessions are cookie-based and terminated at the
 * gateway, so the browser attaches them and nothing here holds a token —
 * a token in JavaScript-reachable storage is a token available to anything
 * that manages to run in the page.
 */
export function createApiClient(options: ApiOptions = {}): RESTClient {
  const config: ClientConfig = {
    baseURL: options.baseUrl ?? "",
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
