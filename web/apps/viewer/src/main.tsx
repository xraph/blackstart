import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "@tanstack/react-router";

import { router } from "./router";
import "./index.css";

const root = document.getElementById("root");
if (!root) {
  throw new Error("viewer: index.html has no #root element to mount into");
}

createRoot(root).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
);
