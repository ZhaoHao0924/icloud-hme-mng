import "./styles/global.css";
import "./styles/app-shell.css";

import { createRoot } from "react-dom/client";

import { StrictMode } from "react";

import { AppProviders } from "./app/AppProviders";
import { router } from "./app/router";
import { RouterProvider } from "react-router-dom";
import { enableMocking } from "./test/mocks/enableMocking";

const root = document.getElementById("root");

if (!root) {
  throw new Error("Missing application root element");
}

async function renderApplication(container: HTMLElement) {
  await enableMocking();

  createRoot(container).render(
    <StrictMode>
      <AppProviders>
        <RouterProvider router={router} />
      </AppProviders>
    </StrictMode>,
  );
}

void renderApplication(root);
