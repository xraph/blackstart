import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "@tanstack/react-router";

import { ApiProvider } from "./api/provider";
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
 * the bundle and the API, so the session cookie is already in scope. A
 * development build points at a host running elsewhere, and that is the only
 * arrangement that needs CORS.
 */
const API_BASE = import.meta.env["VITE_API_BASE_URL"] ?? "";

createRoot(root).render(
  <StrictMode>
    <ApiProvider baseUrl={API_BASE}>
      <RouterProvider router={router} />
    </ApiProvider>
  </StrictMode>,
);
