import { createBrowserRouter, Navigate, type RouteObject } from "react-router-dom";

import { App } from "./App";
import { NotFoundPage } from "./NotFoundPage";
import { PlatformAuthGate } from "./PlatformAuthGate";
import { PlatformLoginPage } from "./PlatformLoginPage";
import { RouteErrorBoundary } from "./RouteErrorBoundary";
import { LoadingState } from "../components/LoadingState";

export const routes: RouteObject[] = [
  {
    path: "/login",
    element: <PlatformLoginPage />,
    errorElement: <RouteErrorBoundary />,
  },
  {
    path: "/",
    element: <PlatformAuthGate />,
    errorElement: <RouteErrorBoundary />,
    hydrateFallbackElement: <LoadingState label="正在加载页面" />,
    children: [
      {
        element: <App />,
        children: [
          { index: true, element: <Navigate replace to="/accounts" /> },
          {
            path: "accounts",
            lazy: async () => {
              const { AccountWorkspace } = await import("../features/accounts/AccountWorkspace");
              return { Component: AccountWorkspace };
            },
          },
          {
            path: "accounts/:accountId",
            lazy: async () => {
              const { AccountDetailLayout } =
                await import("../features/accounts/AccountDetailLayout");
              return { Component: AccountDetailLayout };
            },
            children: [
              { index: true, element: <Navigate replace to="aliases" /> },
              {
                path: "aliases",
                lazy: async () => {
                  const { AliasesPage } = await import("../features/aliases/AliasesPage");
                  return { Component: AliasesPage };
                },
              },
              {
                path: "automation",
                lazy: async () => {
                  const { AliasAutomationPage } =
                    await import("../features/automation/AliasAutomationPage");
                  return { Component: AliasAutomationPage };
                },
              },
              {
                path: "inbox",
                lazy: async () => {
                  const { InboxPage } = await import("../features/inbox/InboxPage");
                  return { Component: InboxPage };
                },
              },
              {
                path: "security",
                lazy: async () => {
                  const { SecurityPage } = await import("../features/security/SecurityPage");
                  return { Component: SecurityPage };
                },
              },
            ],
          },
          {
            path: "settings",
            lazy: async () => {
              const { SettingsPage } = await import("../features/system/SettingsPage");
              return { Component: SettingsPage };
            },
          },
          { path: "*", element: <NotFoundPage /> },
        ],
      },
    ],
  },
];

export const router = createBrowserRouter(routes);
