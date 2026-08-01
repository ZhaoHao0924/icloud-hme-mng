import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { createApiClient } from "../../api/client";
import { accountsQueryOptions, queryKeys } from "../../api/queries";
import { accountFixtures } from "../../test/fixtures";
import { mockScenario } from "../../test/mocks/server";
import { NotificationProvider } from "../../components/NotificationProvider";
import { AccountWorkspace } from "./AccountWorkspace";

function renderWorkspace() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <NotificationProvider>
        <MemoryRouter>
          <AccountWorkspace />
        </MemoryRouter>
      </NotificationProvider>
    </QueryClientProvider>,
  );
  return { ...view, queryClient };
}

describe("AccountWorkspace", () => {
  it("renders account rows and status summaries from the API", async () => {
    renderWorkspace();

    expect(await screen.findByText("主账号")).toBeInTheDocument();
    expect(screen.getByText("待登录账号")).toBeInTheDocument();
    expect(screen.getByText("2 个账户")).toBeInTheDocument();
    expect(screen.getByText("Cookie 已配置")).toBeInTheDocument();
    expect(screen.getByText("App 密码 已配置")).toBeInTheDocument();
    expect(screen.getAllByText("正常").length).toBeGreaterThan(0);
    expect(screen.getAllByText("待登录").length).toBeGreaterThan(0);
    expect(screen.getByText("Cookie 未配置")).toBeInTheDocument();
    expect(screen.getByText("App 密码 未配置")).toBeInTheDocument();
  });

  it("renders the empty state without stale rows", async () => {
    mockScenario.set("empty");
    renderWorkspace();

    expect(await screen.findByRole("heading", { level: 3, name: "暂无账户" })).toBeInTheDocument();
    expect(screen.getByText("0 个账户")).toBeInTheDocument();
    expect(screen.getByText("账户总数").closest(".account-status-item")).toHaveTextContent(
      "账户总数0",
    );
    expect(screen.queryByText("主账号")).not.toBeInTheDocument();
  });

  it("shows a retryable error and refetches on demand", async () => {
    mockScenario.set("error");
    renderWorkspace();

    expect(await screen.findByRole("alert")).toHaveTextContent("Apple 服务错误");
    const retryButton = screen.getByRole("button", { name: "重新加载" });

    mockScenario.set("success");
    await userEvent.setup().click(retryButton);

    await waitFor(() => expect(screen.getByText("主账号")).toBeInTheDocument());
    expect(screen.getByText("2 个账户")).toBeInTheDocument();
  });

  it("shows an offline message and recovers after the local service returns", async () => {
    mockScenario.set("offline");
    renderWorkspace();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "无法连接到本地服务，请确认服务已启动。",
    );
    const retryButton = screen.getByRole("button", { name: "重新加载" });

    mockScenario.set("success");
    await userEvent.setup().click(retryButton);

    expect(await screen.findByText("主账号")).toBeInTheDocument();
    expect(screen.getByText("2 个账户")).toBeInTheDocument();
  });

  it("shows explicit credential capability states and recovery link for an unhealthy account", async () => {
    mockScenario.set("mixed");
    renderWorkspace();

    const accountName = await screen.findByText("需要恢复的账号");
    const accountRow = accountName.closest("tr");
    expect(accountRow).not.toBeNull();
    const row = within(accountRow as HTMLTableRowElement);

    expect(row.getByText("Cookie 已配置")).toBeInTheDocument();
    expect(row.getByText("App 密码 未配置")).toBeInTheDocument();
    expect(row.getByText("Cookie 会话已过期，请更新凭据")).toBeInTheDocument();
    expect(row.getByRole("link", { name: "更新 Cookie" })).toHaveAttribute(
      "href",
      "/accounts/acc_error/security",
    );
  });

  it("validates and creates an account, then refreshes the list", async () => {
    mockScenario.set("empty");
    const user = userEvent.setup();
    const { queryClient } = renderWorkspace();
    const proxyPassword = "fe206-proxy-password";
    const proxyURL = `http://proxy-user:${proxyPassword}@127.0.0.1:7890`;

    await screen.findByRole("heading", { level: 3, name: "暂无账户" });
    await user.click(screen.getByRole("button", { name: "添加账户" }));
    const dialog = screen.getByRole("dialog", { name: "添加账户" });

    await user.click(within(dialog).getByRole("button", { name: "添加账户" }));
    expect(within(dialog).getByText("请输入账户名称")).toBeInTheDocument();
    expect(within(dialog).getByText("请输入 iCloud 邮箱")).toBeInTheDocument();

    await user.type(within(dialog).getByLabelText("账户名称"), "工作账号");
    await user.type(within(dialog).getByLabelText("iCloud 邮箱"), "work@icloud.com");
    await user.selectOptions(within(dialog).getByLabelText("区域"), "icloud.com.cn");
    await user.type(within(dialog).getByLabelText("代理（可选）"), proxyURL);
    await user.click(within(dialog).getByRole("button", { name: "添加账户" }));

    expect(await screen.findByText("工作账号")).toBeInTheDocument();
    expect(screen.getByText("work@icloud.com")).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: "添加账户" })).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("已添加账户“工作账号”");

    const mutation = queryClient.getMutationCache().getAll().at(-1);
    expect(mutation?.state.variables).toBeUndefined();
    expect(JSON.stringify(mutation?.state)).not.toContain(proxyPassword);
    expect(JSON.stringify(queryClient.getQueryData(queryKeys.accounts))).not.toContain(
      proxyPassword,
    );
    expect(document.body).not.toHaveTextContent(proxyPassword);
    expect(window.location.href).not.toContain(proxyPassword);
  });

  it("keeps account form values when the server rejects creation", async () => {
    mockScenario.set("error");
    const user = userEvent.setup();
    const { queryClient } = renderWorkspace();
    const proxyPassword = "fe206-retry-proxy-password";
    const proxyURL = `http://proxy-user:${proxyPassword}@127.0.0.1:7890`;

    await screen.findByRole("alert");
    await user.click(screen.getByRole("button", { name: "添加账户" }));
    const dialog = screen.getByRole("dialog", { name: "添加账户" });
    const nameInput = within(dialog).getByLabelText("账户名称");
    const emailInput = within(dialog).getByLabelText("iCloud 邮箱");
    const proxyInput = within(dialog).getByLabelText("代理（可选）");

    await user.type(nameInput, "保留输入的账号");
    await user.type(emailInput, "retry@icloud.com");
    await user.type(proxyInput, proxyURL);
    await user.click(within(dialog).getByRole("button", { name: "添加账户" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Apple 服务错误");
    expect(nameInput).toHaveValue("保留输入的账号");
    expect(emailInput).toHaveValue("retry@icloud.com");
    expect(proxyInput).toHaveValue(proxyURL);
    const mutation = queryClient.getMutationCache().getAll().at(-1);
    expect(mutation?.state.variables).toBeUndefined();
    expect(JSON.stringify(mutation?.state)).not.toContain(proxyPassword);
    expect(
      JSON.stringify(
        queryClient
          .getQueryCache()
          .getAll()
          .map((query) => query.state),
      ),
    ).not.toContain(proxyPassword);
    expect(window.location.href).not.toContain(proxyPassword);
  });

  it("opens a destructive confirmation with the selected account name", async () => {
    const user = userEvent.setup();
    renderWorkspace();

    await screen.findByText("主账号");
    await user.click(screen.getByRole("button", { name: "删除账户 主账号" }));

    const dialog = screen.getByRole("alertdialog", { name: "确认删除账户？" });
    expect(dialog).toHaveTextContent("删除“主账号”后，本地配置和登录状态都会移除，且无法恢复。");
    expect(within(dialog).getByRole("button", { name: "删除账户" })).toBeInTheDocument();
  });

  it("removes a deleted account and refreshes the status summary", async () => {
    const user = userEvent.setup();
    renderWorkspace();

    await screen.findByText("主账号");
    await user.click(screen.getByRole("button", { name: "删除账户 主账号" }));
    const dialog = screen.getByRole("alertdialog", { name: "确认删除账户？" });
    await user.click(within(dialog).getByRole("button", { name: "删除账户" }));

    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "删除账户 主账号" })).not.toBeInTheDocument(),
    );
    expect(screen.getByText("1 个账户")).toBeInTheDocument();
    expect(screen.getByText("账户总数").closest(".account-status-item")).toHaveTextContent(
      "账户总数1",
    );
    expect(screen.getByText("正常").closest(".account-status-item")).toHaveTextContent("正常0");
    expect(screen.queryByRole("alertdialog", { name: "确认删除账户？" })).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("账户已删除主账号");
  });

  it("keeps the account and confirmation open when deletion fails", async () => {
    const user = userEvent.setup();
    renderWorkspace();

    await screen.findByText("主账号");
    await user.click(screen.getByRole("button", { name: "删除账户 主账号" }));
    const dialog = screen.getByRole("alertdialog", { name: "确认删除账户？" });

    mockScenario.set("error");
    await user.click(within(dialog).getByRole("button", { name: "删除账户" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Apple 服务错误");
    expect(screen.getByRole("alertdialog", { name: "确认删除账户？" })).toBeInTheDocument();
    expect(screen.getByText("主账号")).toBeInTheDocument();
    expect(screen.getByText("2 个账户")).toBeInTheDocument();
  });
});

describe("account presentation contract", () => {
  it("keeps API fixtures free of sensitive credential fields", () => {
    expect(accountFixtures[0]).not.toHaveProperty("cookies");
    expect(accountFixtures[0]).not.toHaveProperty("app_password");
    expect(accountsQueryOptions(createApiClient())).toHaveProperty("queryKey");
  });
});
