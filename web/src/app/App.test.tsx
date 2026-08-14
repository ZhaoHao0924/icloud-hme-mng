import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { AppProviders } from "./AppProviders";
import { createQueryClient } from "./queryClient";
import { routes } from "./router";
import { mockScenario } from "../test/mocks/server";

function renderAccounts() {
  const testRouter = createMemoryRouter(routes, { initialEntries: ["/accounts"] });
  return render(
    <AppProviders queryClient={createQueryClient()}>
      <RouterProvider router={testRouter} />
    </AppProviders>,
  );
}

function renderRoute(path: string) {
  const testRouter = createMemoryRouter(routes, { initialEntries: [path] });
  render(
    <AppProviders queryClient={createQueryClient()}>
      <RouterProvider router={testRouter} />
    </AppProviders>,
  );
  return testRouter;
}

describe("App", () => {
  it("renders the account workspace shell", async () => {
    renderAccounts();

    await screen.findByRole("table", { name: "账户列表" });
    expect(screen.getByRole("heading", { level: 1, name: "账户" })).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "主导航" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "账户" })).toHaveAttribute("href", "/accounts");
  });

  it("expands and collapses the mobile primary menu", async () => {
    const user = userEvent.setup();
    renderAccounts();
    await screen.findByRole("table", { name: "账户列表" });

    const menuButton = screen.getByRole("button", { name: "展开主菜单" });
    const navigation = screen.getByRole("navigation", { name: "主导航" });
    expect(menuButton).toHaveAttribute("aria-expanded", "false");
    expect(navigation).not.toHaveClass("primary-nav-open");

    await user.click(menuButton);
    expect(screen.getByRole("button", { name: "收起主菜单" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(navigation).toHaveClass("primary-nav-open");

    await user.keyboard("{Escape}");
    expect(screen.getByRole("button", { name: "展开主菜单" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    expect(navigation).not.toHaveClass("primary-nav-open");
  });

  it("renders an accessible empty account state", async () => {
    mockScenario.set("empty");
    renderAccounts();

    expect(await screen.findByRole("table", { name: "账户列表" })).toBeInTheDocument();
    expect(await screen.findByRole("heading", { level: 3, name: "暂无账户" })).toBeInTheDocument();
    expect(screen.getByText("0 个账户")).toBeInTheDocument();
  });

  it("renders the not-found page for an unknown route", async () => {
    const testRouter = createMemoryRouter(routes, { initialEntries: ["/missing"] });
    render(
      <AppProviders queryClient={createQueryClient()}>
        <RouterProvider router={testRouter} />
      </AppProviders>,
    );

    expect(
      await screen.findByRole("heading", { level: 2, name: "找不到页面" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "返回账户" })).toHaveAttribute("href", "/accounts");
  });

  it("opens an account with shared context and navigates detail tabs", async () => {
    const user = userEvent.setup();
    const testRouter = renderRoute("/accounts");

    await user.click(await screen.findByRole("link", { name: "打开账户 主账号" }));

    await waitFor(() =>
      expect(testRouter.state.location.pathname).toBe("/accounts/acc_primary/aliases"),
    );
    expect(await screen.findByRole("heading", { level: 2, name: "主账号" })).toBeInTheDocument();
    expect(document.querySelector(".account-context-meta")).toHaveTextContent(
      "primary@icloud.com · acc_primary",
    );
    const detailNavigation = await screen.findByRole("navigation", { name: "主账号详情导航" });
    expect(within(detailNavigation).getByRole("link", { name: "别名" })).toHaveAttribute(
      "aria-current",
      "page",
    );

    await user.click(within(detailNavigation).getByRole("link", { name: "收件箱" }));
    await waitFor(() =>
      expect(testRouter.state.location.pathname).toBe("/accounts/acc_primary/inbox"),
    );
    expect(await screen.findByRole("heading", { level: 1, name: "收件箱" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "主账号" })).toBeInTheDocument();
    expect(within(detailNavigation).getByRole("link", { name: "收件箱" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("recovers an expired account through credentials and returns to the source page", async () => {
    const user = userEvent.setup();
    mockScenario.set("mixed");
    const testRouter = renderRoute("/accounts");
    const accountRow = (await screen.findByText("需要恢复的账号")).closest("tr");
    expect(accountRow).not.toBeNull();

    await user.click(
      within(accountRow as HTMLTableRowElement).getByRole("link", {
        name: "更新 Cookie",
      }),
    );

    await waitFor(() =>
      expect(testRouter.state.location.pathname).toBe("/accounts/acc_error/security"),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent("Cookie 会话已过期");
    const cookieInput = await screen.findByLabelText("Cookie 数据");
    await waitFor(() => expect(cookieInput).toHaveFocus());

    await user.type(cookieInput, "session=recovered-session-value");
    await user.click(screen.getByRole("button", { name: "更新 Cookie" }));

    await waitFor(() => expect(testRouter.state.location.pathname).toBe("/accounts"));
    expect(testRouter.state.location.state).toBeNull();
    expect(await screen.findByRole("status")).toHaveTextContent("Cookie 已更新");
    expect(document.body).not.toHaveTextContent("recovered-session-value");
  });

  it("redirects an unknown account to the workspace and announces the error", async () => {
    const testRouter = renderRoute("/accounts/missing/aliases");

    await waitFor(() => expect(testRouter.state.location.pathname).toBe("/accounts"));
    expect(await screen.findByRole("heading", { level: 2, name: "所有账户" })).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("账户不存在");
    expect(screen.getByRole("alert")).toHaveTextContent("请从账户列表重新选择一个账户");
  });
});
