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
 * The API base.
 *
 * Empty means same origin, which is the deployed case: the gateway serves both
 * the bundle and the API. A development build points at a host running
 * elsewhere, and that is the only arrangement that needs CORS.
 */
const API_BASE = import.meta.env["VITE_API_BASE_URL"] ?? "";

/**
 * Which Authsome app a credential belongs to.
 *
 * Publishable by design — it names the app and grants nothing on its own,
 * which is why it can sit in a bundle where a secret could not. The platform
 * issues it at first boot; it is also readable from the authsome_apps table.
 */
const PUBLISHABLE_KEY = import.meta.env["VITE_AUTHSOME_PUBLISHABLE_KEY"] ?? "";

/**
 * The console, once somebody is signed in.
 *
 * The API client is built from the session, so it does not exist before there
 * is one. That is deliberate: a console mounted without a credential issues a
 * screenful of requests that all answer 401, and the reader ends up reading
 * error panels instead of a login form.
 */
function Console(): React.JSX.Element {
  const { session } = useAuth();

  if (session === null) {
    return <LoginScreen />;
  }

  return (
    <ApiProvider baseUrl={API_BASE} token={session.token}>
      <RouterProvider router={router} />
    </ApiProvider>
  );
}

createRoot(root).render(
  <StrictMode>
    <AuthProvider publishableKey={PUBLISHABLE_KEY} baseUrl={API_BASE}>
      <Console />
    </AuthProvider>
  </StrictMode>,
);
