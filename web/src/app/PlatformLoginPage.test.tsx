import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { queryKeys } from "../api/queries";
import { mockScenario } from "../test/mocks/server";
import { AppProviders } from "./AppProviders";
import { createQueryClient } from "./queryClient";
import { routes } from "./router";

function renderProtectedRoute(path: string) {
  const queryClient = createQueryClient();
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(
    <AppProviders queryClient={queryClient}>
      <RouterProvider router={router} />
    </AppProviders>,
  );
  return { queryClient, router };
}

describe("PlatformLoginPage", () => {
  it("requires login before rendering a protected route and clears access on logout", async () => {
    const user = userEvent.setup();
    mockScenario.set("platform-login");
    const { queryClient, router } = renderProtectedRoute(
      "/accounts/acc_primary/inbox?source=workspace",
    );

    await expect(
      screen.findByRole("heading", { level: 1, name: "登录平台" }),
    ).resolves.toBeVisible();
    expect(screen.queryByRole("table", { name: "账户列表" })).not.toBeInTheDocument();

    const password = "correct-horse-battery-staple";
    await user.type(screen.getByLabelText("管理员密码"), password);
    await user.click(screen.getByRole("button", { name: "登录" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/accounts/acc_primary/inbox"));
    expect(await screen.findByRole("heading", { level: 1, name: "收件箱" })).toBeInTheDocument();
    expect(queryClient.getQueryData(queryKeys.platformAuth)).toMatchObject({ authenticated: true });
    expect(JSON.stringify(queryClient.getMutationCache().getAll())).not.toContain(password);
    expect(document.body).not.toHaveTextContent(password);

    await user.click(screen.getByRole("button", { name: "退出登录" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(await screen.findByRole("heading", { level: 1, name: "登录平台" })).toBeInTheDocument();
    expect(queryClient.getQueryData(queryKeys.platformAuth)).toMatchObject({
      authenticated: false,
    });
    expect(screen.queryByRole("heading", { level: 1, name: "收件箱" })).not.toBeInTheDocument();
  });

  it("guides an unconfigured service through administrator setup", async () => {
    const user = userEvent.setup();
    mockScenario.set("platform-setup");
    const { router } = renderProtectedRoute("/accounts");

    expect(
      await screen.findByRole("heading", { level: 1, name: "创建管理员账户" }),
    ).toBeInTheDocument();
    await user.type(screen.getByLabelText("管理员密码"), "correct-horse-battery-staple");
    await user.type(screen.getByLabelText("确认管理员密码"), "correct-horse-battery-staple");
    await user.click(screen.getByRole("button", { name: "创建并进入平台" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/accounts"));
    expect(await screen.findByRole("table", { name: "账户列表" })).toBeInTheDocument();
  });
});
