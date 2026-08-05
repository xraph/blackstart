import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

/**
 * The host this development server stands in front of.
 *
 * A proxy rather than an absolute API base, and that is a security decision as
 * much as a convenience one. The session is an HttpOnly cookie, and a cookie
 * reaches a same-origin request without anything being configured — `fetch`
 * defaults to `credentials: "same-origin"`, so the generated client, which is
 * written by `forge client generate` and never edited by hand, needs no change
 * at all. Pointing the client at `http://localhost:8080` instead would make
 * every call cross-origin, which needs `credentials: "include"` here and a
 * credentialed CORS surface on the host — two things to configure and keep in
 * step in order to arrive back where this starts.
 *
 * It also makes development resemble deployment, where the gateway serves the
 * bundle and the API from one origin.
 */
/* Read narrowly rather than by adding @types/node. This is a browser app, and
 * pulling Node's globals into its type environment to reach one variable in
 * one config file would make `process` look available to every other file. */
const env =
  (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env ?? {};

const HOST = env["VITE_API_PROXY_TARGET"] ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      // The grid API and the identity API are separate roots on the host —
      // Authsome is mounted outside /api/v1 so a generated client for the grid
      // is not mostly identity — so both are named here.
      //
      // `changeOrigin` is deliberately off. It rewrites the Host header, and
      // the host resolves cookie domain and app context from the request; a
      // rewritten Host works until the cookie domain setting is non-empty and
      // then fails as a session that will not persist, which is a long way
      // from its cause.
      "/api": { target: HOST },
      "/identity": { target: HOST },

      // The signup form's field list comes from Authsome's dashboard-contract
      // intent, which is mounted under the dashboard's base path rather than
      // under /identity. Same host, third root — see auth/formconfig.ts for
      // why the config is read there and the submission still goes to
      // /identity.
      "/_forge": { target: HOST },
    },
  },
});
