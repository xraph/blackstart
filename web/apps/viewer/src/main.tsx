import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "@tanstack/react-router";

import { ApiProvider } from "./api/provider";
import { AuthProvider, useAuth } from "./auth/provider";
import { LoginScreen } from "./auth/LoginScreen";
import { router } from "./router";
import "./index.css";

const root = document.getElementById("root");
if (!root) {
  throw new Error("viewer: index.html has no #root element to mount into");
}

/**
 * Which Authsome app a credential belongs to.
 *
 * Publishable by design — it names the app and grants nothing on its own,
 * which is why it can sit in a bundle where a secret could not. The platform
 * issues it at first boot; it is also readable from the authsome_apps table.
 */
const PUBLISHABLE_KEY = import.meta.env["VITE_AUTHSOME_PUBLISHABLE_KEY"] ?? "";

/**
 * What the page shows while `/me` is still answering.
 *
 * Not a blank page. A white screen for the length of a cold request reads as a
 * failed build, and this is the first thing anyone sees.
 */
function Checking(): React.JSX.Element {
  return (
    <div className="flex h-full items-center justify-center bg-bg">
      <span role="status" className="font-mono text-[11.5px] font-bold tracking-[0.16em] text-fg-3">
        BLACKSTART
      </span>
    </div>
  );
}

/**
 * The console, once somebody is signed in.
 *
 * Three states, because there are three. Rendering the login form during
 * `checking` would flash it at an operator who is already signed in on every
 * reload, and rendering the router would issue a screenful of requests that
 * all answer 401.
 *
 * Note what does not happen here: no redirect. The address bar is never
 * rewritten, so a link carrying `?valid=…&sel=…` is still exactly itself after
 * a sign-in — the citable URL survives by not being touched rather than by
 * being carefully preserved.
 */
function Console(): React.JSX.Element {
  const { status, expire } = useAuth();

  if (status === "checking") return <Checking />;
  if (status === "anonymous") return <LoginScreen />;

  return (
    <ApiProvider onExpired={expire}>
      <RouterProvider router={router} />
    </ApiProvider>
  );
}

createRoot(root).render(
  <StrictMode>
    <AuthProvider publishableKey={PUBLISHABLE_KEY}>
      <Console />
    </AuthProvider>
  </StrictMode>,
);
