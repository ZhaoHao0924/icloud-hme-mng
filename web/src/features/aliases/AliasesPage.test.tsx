import { HttpResponse, http } from "msw";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { AppProviders } from "../../app/AppProviders";
import { createQueryClient } from "../../app/queryClient";
import { routes } from "../../app/router";
import { aliasFixtures } from "../../test/fixtures";
import { mockAliases, server } from "../../test/mocks/server";
import { readSessionRecoveryLocationState } from "../security/sessionRecoveryState";

function renderAliases(path = "/accounts/acc_primary/aliases") {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  const queryClient = createQueryClient();
  const view = render(
    <AppProviders queryClient={queryClient}>
      <RouterProvider router={router} />
    </AppProviders>,
  );
  return { ...view, queryClient, router };
}

describe("AliasesPage", () => {
  it("renders alias identity, labels, status, and counts for the current account", async () => {
    renderAliases();

    const table = await screen.findByRole("table", { name: "别名列表" }, { timeout: 3_000 });
    expect(screen.getByText("2 个别名")).toBeInTheDocument();
    const activeRow = within(table).getByText("quiet-orchid@icloud.com").closest("tr");
    const inactiveRow = within(table).getByText("silver-field@icloud.com").closest("tr");
    expect(activeRow).not.toBeNull();
    expect(inactiveRow).not.toBeNull();
    expect(within(activeRow as HTMLTableRowElement).getByText("GitHub")).toBeInTheDocument();
    expect(within(activeRow as HTMLTableRowElement).getByText("使用中")).toBeInTheDocument();
    expect(
      within(activeRow as HTMLTableRowElement).getByRole("button", {
        name: "复制邮箱 quiet-orchid@icloud.com",
      }),
    ).toBeInTheDocument();
    expect(
      within(activeRow as HTMLTableRowElement).getByRole("button", {
        name: "停用别名 quiet-orchid@icloud.com",
      }),
    ).toBeInTheDocument();
    expect(within(inactiveRow as HTMLTableRowElement).getByText("旧服务")).toBeInTheDocument();
    expect(within(inactiveRow as HTMLTableRowElement).getByText("已停用")).toBeInTheDocument();
    expect(
      within(inactiveRow as HTMLTableRowElement).getByRole("button", {
        name: "恢复别名 silver-field@icloud.com",
      }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "全部" })).toHaveAttribute("aria-pressed", "true");
  });

  it("sorts aliases by creation time and paginates the filtered list", async () => {
    const aliases = Array.from({ length: 25 }, (_, index) => ({
      ...aliasFixtures[0],
      anonymousId: `page-alias-${index + 1}`,
      createdAt: new Date(Date.UTC(2026, 7, 25 - index)).toISOString(),
      email: `page-alias-${index + 1}@icloud.com`,
      label: `Page ${index + 1}`,
    }));
    server.use(
      http.get("*/api/aliases", ({ request }) => {
        const accountId = new URL(request.url).searchParams.get("account_id") ?? "";
        return HttpResponse.json({
          data: { account_id: accountId, aliases: [...aliases].reverse(), count: aliases.length },
          success: true,
        });
      }),
    );
    const user = userEvent.setup();
    const { router } = renderAliases();

    const table = await screen.findByRole("table", { name: "别名列表" });
    expect(within(table).getAllByRole("row")).toHaveLength(11);
    expect(within(table).getByText("page-alias-1@icloud.com")).toBeInTheDocument();
    expect(within(table).queryByText("page-alias-11@icloud.com")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "下一页" }));

    expect(within(table).getAllByRole("row")).toHaveLength(11);
    expect(within(table).getByText("page-alias-11@icloud.com")).toBeInTheDocument();
    expect(within(table).queryByText("page-alias-1@icloud.com")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "下一页" })).toBeEnabled();
    expect(new URLSearchParams(router.state.location.search).get("page")).toBe("2");

    await user.click(screen.getByRole("button", { name: "下一页" }));

    expect(within(table).getAllByRole("row")).toHaveLength(6);
    expect(within(table).getByText("page-alias-21@icloud.com")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
    expect(new URLSearchParams(router.state.location.search).get("page")).toBe("3");
  });

  it("refreshes the server list after stopping and restoring an alias", async () => {
    const user = userEvent.setup();
    renderAliases();

    const table = await screen.findByRole("table", { name: "别名列表" });
    const activeRow = within(table).getByText("quiet-orchid@icloud.com").closest("tr");
    expect(activeRow).not.toBeNull();

    await user.click(
      within(activeRow as HTMLTableRowElement).getByRole("button", {
        name: "停用别名 quiet-orchid@icloud.com",
      }),
    );

    await waitFor(() =>
      expect(
        within(activeRow as HTMLTableRowElement).getByRole("button", {
          name: "恢复别名 quiet-orchid@icloud.com",
        }),
      ).toBeInTheDocument(),
    );
    expect(within(activeRow as HTMLTableRowElement).getByText("已停用")).toBeInTheDocument();
    expect(screen.getByText("别名已停用")).toBeInTheDocument();
    expect(screen.getByText("2 个别名")).toBeInTheDocument();

    await user.click(
      within(activeRow as HTMLTableRowElement).getByRole("button", {
        name: "恢复别名 quiet-orchid@icloud.com",
      }),
    );

    await waitFor(() =>
      expect(
        within(activeRow as HTMLTableRowElement).getByRole("button", {
          name: "停用别名 quiet-orchid@icloud.com",
        }),
      ).toBeInTheDocument(),
    );
    expect(within(activeRow as HTMLTableRowElement).getByText("使用中")).toBeInTheDocument();
    expect(screen.getByText("别名已恢复")).toBeInTheDocument();
  });

  it("opens an explicit destructive confirmation before deleting an alias", async () => {
    let deleteAttempts = 0;
    server.use(
      http.delete("*/api/aliases/:aliasId", () => {
        deleteAttempts += 1;
        return HttpResponse.json({ data: { anonymous_id: "alias_active_1" }, success: true });
      }),
    );
    const user = userEvent.setup();
    renderAliases();

    await screen.findByRole("table", { name: "别名列表" });
    await user.click(screen.getByRole("button", { name: "删除别名 quiet-orchid@icloud.com" }));

    const dialog = screen.getByRole("alertdialog", { name: "确认删除别名？" });
    expect(dialog).toHaveTextContent(
      "删除“quiet-orchid@icloud.com”后，此 Hide My Email 别名将从账户中移除，且无法恢复。",
    );
    expect(within(dialog).getByRole("button", { name: "删除别名" })).toBeInTheDocument();

    await user.click(within(dialog).getByRole("button", { name: "取消" }));
    expect(screen.queryByRole("alertdialog", { name: "确认删除别名？" })).not.toBeInTheDocument();
    expect(screen.getByText("quiet-orchid@icloud.com")).toBeInTheDocument();
    expect(deleteAttempts).toBe(0);
  });

  it("deletes an alias after confirmation and refreshes the server list", async () => {
    const user = userEvent.setup();
    renderAliases();

    const table = await screen.findByRole("table", { name: "别名列表" });
    await user.click(
      within(table).getByRole("button", { name: "删除别名 quiet-orchid@icloud.com" }),
    );
    const dialog = screen.getByRole("alertdialog", { name: "确认删除别名？" });
    await user.click(within(dialog).getByRole("button", { name: "删除别名" }));

    await waitFor(() =>
      expect(within(table).queryByText("quiet-orchid@icloud.com")).not.toBeInTheDocument(),
    );
    expect(screen.getByText("1 个别名")).toBeInTheDocument();
    expect(screen.getByText("silver-field@icloud.com")).toBeInTheDocument();
    expect(screen.queryByRole("alertdialog", { name: "确认删除别名？" })).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("别名已删除quiet-orchid@icloud.com");
  });

  it("locks alias delete confirmation while pending and prevents duplicate submission", async () => {
    let deleteAttempts = 0;
    let resolveDelete: ((response: Response) => void) | undefined;
    server.use(
      http.delete("*/api/aliases/:aliasId", async ({ params, request }) => {
        deleteAttempts += 1;
        const body = (await request.json()) as { account_id?: unknown };
        const accountId = typeof body.account_id === "string" ? body.account_id : "";
        const aliasId = String(params.aliasId);
        return new Promise<Response>((resolve) => {
          resolveDelete = (response) => {
            mockAliases.delete(accountId, aliasId, aliasFixtures);
            resolve(response);
          };
        });
      }),
    );
    const user = userEvent.setup();
    renderAliases();

    const table = await screen.findByRole("table", { name: "别名列表" });
    await user.click(screen.getByRole("button", { name: "删除别名 quiet-orchid@icloud.com" }));
    const dialog = screen.getByRole("alertdialog", { name: "确认删除别名？" });
    await user.click(within(dialog).getByRole("button", { name: "删除别名" }));

    expect(await within(dialog).findByRole("button", { name: "处理中" })).toBeDisabled();
    expect(within(dialog).getByRole("button", { name: "取消" })).toBeDisabled();
    await user.keyboard("{Escape}");
    expect(screen.getByRole("alertdialog", { name: "确认删除别名？" })).toBeInTheDocument();
    expect(deleteAttempts).toBe(1);

    resolveDelete?.(HttpResponse.json({ data: { anonymous_id: "alias_active_1" }, success: true }));
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog", { name: "确认删除别名？" })).not.toBeInTheDocument(),
    );
    await waitFor(() =>
      expect(within(table).queryByText("quiet-orchid@icloud.com")).not.toBeInTheDocument(),
    );
  });

  it("keeps the alias and confirmation open when deletion fails without automatic retries", async () => {
    let deleteAttempts = 0;
    server.use(
      http.delete("*/api/aliases/:aliasId", () => {
        deleteAttempts += 1;
        return HttpResponse.json({ message: "模拟删除失败", success: false }, { status: 502 });
      }),
    );
    const user = userEvent.setup();
    renderAliases();

    const table = await screen.findByRole("table", { name: "别名列表" });
    await user.click(
      within(table).getByRole("button", { name: "删除别名 quiet-orchid@icloud.com" }),
    );
    const dialog = screen.getByRole("alertdialog", { name: "确认删除别名？" });
    await user.click(within(dialog).getByRole("button", { name: "删除别名" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("模拟删除失败");
    expect(deleteAttempts).toBe(1);
    expect(screen.getByRole("alertdialog", { name: "确认删除别名？" })).toBeInTheDocument();
    expect(screen.getByText("quiet-orchid@icloud.com")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "删除别名" })).toBeEnabled();
  });

  it("keeps alias status unchanged and avoids automatic retries when an action fails", async () => {
    let actionAttempts = 0;
    server.use(
      http.post("*/api/aliases/:aliasId/deactivate", () => {
        actionAttempts += 1;
        return HttpResponse.json({ message: "模拟停用失败", success: false }, { status: 502 });
      }),
    );
    const user = userEvent.setup();
    renderAliases();

    const table = await screen.findByRole("table", { name: "别名列表" });
    const activeRow = within(table).getByText("quiet-orchid@icloud.com").closest("tr");
    expect(activeRow).not.toBeNull();
    await user.click(
      within(activeRow as HTMLTableRowElement).getByRole("button", {
        name: "停用别名 quiet-orchid@icloud.com",
      }),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent("模拟停用失败");
    expect(actionAttempts).toBe(1);
    expect(within(activeRow as HTMLTableRowElement).getByText("使用中")).toBeInTheDocument();
    expect(
      within(activeRow as HTMLTableRowElement).getByRole("button", {
        name: "停用别名 quiet-orchid@icloud.com",
      }),
    ).toBeEnabled();
  });

  it("combines search and status filters while keeping them in the URL", async () => {
    const user = userEvent.setup();
    const { router } = renderAliases("/accounts/acc_primary/aliases?source=workspace");
    const searchInput = await screen.findByRole("searchbox", { name: "搜索别名" });

    await user.type(searchInput, "GitHub");
    expect(screen.getByText("quiet-orchid@icloud.com")).toBeInTheDocument();
    expect(screen.queryByText("silver-field@icloud.com")).not.toBeInTheDocument();
    expect(screen.getByText("1 / 2 个别名")).toBeInTheDocument();
    expect(new URLSearchParams(router.state.location.search).get("q")).toBe("GitHub");
    expect(new URLSearchParams(router.state.location.search).get("source")).toBe("workspace");

    await user.click(screen.getByRole("button", { name: "清除搜索" }));
    await user.click(screen.getByRole("button", { name: "已停用" }));
    expect(screen.queryByText("quiet-orchid@icloud.com")).not.toBeInTheDocument();
    expect(screen.getByText("silver-field@icloud.com")).toBeInTheDocument();
    expect(new URLSearchParams(router.state.location.search).get("status")).toBe("inactive");

    await user.type(searchInput, "GitHub");
    expect(screen.getByRole("heading", { level: 3, name: "没有匹配的别名" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "清除筛选" }));

    expect(searchInput).toHaveValue("");
    expect(screen.getByText("quiet-orchid@icloud.com")).toBeInTheDocument();
    expect(screen.getByText("silver-field@icloud.com")).toBeInTheDocument();
    expect(router.state.location.search).toBe("?source=workspace");
  });

  it("shows an explicit empty state when the account has no aliases", async () => {
    server.use(
      http.get("*/api/aliases", ({ request }) => {
        const accountId = new URL(request.url).searchParams.get("account_id") ?? "";
        return HttpResponse.json({
          data: { account_id: accountId, aliases: [], count: 0 },
          success: true,
        });
      }),
    );
    renderAliases();

    expect(await screen.findByRole("heading", { level: 3, name: "暂无别名" })).toBeInTheDocument();
    expect(screen.getByText("此账户当前没有 Hide My Email 别名。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "创建别名" })).toBeInTheDocument();
    expect(screen.queryByRole("searchbox", { name: "搜索别名" })).not.toBeInTheDocument();
    expect(screen.queryByRole("table", { name: "别名列表" })).not.toBeInTheDocument();
  });

  it("creates an alias, refreshes the server list, and clears filters that would hide it", async () => {
    const user = userEvent.setup();
    const { router } = renderAliases(
      "/accounts/acc_primary/aliases?source=workspace&q=missing&status=inactive",
    );

    expect(
      await screen.findByRole("heading", { level: 3, name: "没有匹配的别名" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "创建别名" }));
    const dialog = screen.getByRole("dialog", { name: "创建别名" });
    const labelInput = within(dialog).getByLabelText("标签（可选）");
    await user.type(labelInput, "  新闻订阅  ");
    await user.click(within(dialog).getByRole("button", { name: "创建别名" }));

    const table = await screen.findByRole("table", { name: "别名列表" });
    const createdEmail = await within(table).findByText("new-alias@icloud.com");
    const createdRow = createdEmail.closest("tr");
    expect(createdRow).not.toBeNull();
    expect(within(createdRow as HTMLTableRowElement).getByText("新闻订阅")).toBeInTheDocument();
    expect(screen.getByText("3 个别名")).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: "创建别名" })).not.toBeInTheDocument();
    expect(screen.getByText("别名已创建")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("new-alias@icloud.com");
    expect(router.state.location.search).toBe("?source=workspace");
  });

  it("keeps the label available after a create failure and does not retry automatically", async () => {
    let createAttempts = 0;
    server.use(
      http.post("*/api/create", () => {
        createAttempts += 1;
        return HttpResponse.json({ message: "模拟创建失败", success: false }, { status: 502 });
      }),
    );
    const user = userEvent.setup();
    renderAliases();

    await screen.findByRole("table", { name: "别名列表" });
    await user.click(screen.getByRole("button", { name: "创建别名" }));
    const dialog = screen.getByRole("dialog", { name: "创建别名" });
    const labelInput = within(dialog).getByLabelText("标签（可选）");
    await user.type(labelInput, "失败后保留");
    await user.click(within(dialog).getByRole("button", { name: "创建别名" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent("模拟创建失败");
    expect(labelInput).toHaveValue("失败后保留");
    expect(createAttempts).toBe(1);
    expect(screen.getByText("2 个别名")).toBeInTheDocument();
  });

  it("creates the first alias from the empty state without requiring a label", async () => {
    server.use(
      http.get("*/api/aliases", ({ request }) => {
        const accountId = new URL(request.url).searchParams.get("account_id") ?? "";
        const aliases = mockAliases.list(accountId);
        return HttpResponse.json({
          data: { account_id: accountId, aliases, count: aliases.length },
          success: true,
        });
      }),
    );
    const user = userEvent.setup();
    renderAliases();

    await screen.findByRole("heading", { level: 3, name: "暂无别名" });
    await user.click(screen.getByRole("button", { name: "创建别名" }));
    const dialog = screen.getByRole("dialog", { name: "创建别名" });
    await user.click(within(dialog).getByRole("button", { name: "创建别名" }));

    const table = await screen.findByRole("table", { name: "别名列表" });
    expect(await within(table).findByText("new-alias@icloud.com")).toBeInTheDocument();
    expect(screen.getByText("未设置标签")).toBeInTheDocument();
    expect(screen.getByText("1 个别名")).toBeInTheDocument();
  });

  it("keeps alias failures retryable without losing the account context", async () => {
    let shouldFail = true;
    server.use(
      http.get("*/api/aliases", ({ request }) => {
        if (shouldFail) {
          return HttpResponse.json(
            { message: "模拟别名服务错误", success: false },
            { status: 502 },
          );
        }
        const accountId = new URL(request.url).searchParams.get("account_id") ?? "";
        return HttpResponse.json({
          data: { account_id: accountId, aliases: aliasFixtures, count: aliasFixtures.length },
          success: true,
        });
      }),
    );
    const user = userEvent.setup();
    renderAliases();

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Apple 服务错误：模拟别名服务错误");
    expect(screen.getByRole("heading", { level: 2, name: "主账号" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "更新 Cookie" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "创建别名" })).not.toBeInTheDocument();

    shouldFail = false;
    await user.click(screen.getByRole("button", { name: "重新加载" }));

    await waitFor(() =>
      expect(screen.getByRole("table", { name: "别名列表" })).toBeInTheDocument(),
    );
  });

  it("routes alias session expiration to credentials with the alias page source", async () => {
    server.use(
      http.get("*/api/aliases", () =>
        HttpResponse.json(
          { message: "iCloud 会话失效，请更新 Cookie", success: false },
          { status: 401 },
        ),
      ),
    );
    const user = userEvent.setup();
    const { router } = renderAliases("/accounts/acc_primary/aliases?status=active&q=GitHub");

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Cookie 会话已过期");
    expect(alert).toHaveTextContent("会话已过期，请更新 Cookie。");
    expect(screen.queryByRole("button", { name: "重新加载" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("link", { name: "更新 Cookie" }));

    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/accounts/acc_primary/security"),
    );
    expect(readSessionRecoveryLocationState(router.state.location.state)).toEqual({
      from: "/accounts/acc_primary/aliases?status=active&q=GitHub",
      reason: "icloud_session_expired",
    });
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("更新 Cookie 或重新登录后将返回原页面。"),
    );
  });
});
