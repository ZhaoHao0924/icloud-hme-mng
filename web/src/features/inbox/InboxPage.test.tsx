import { HttpResponse, http } from "msw";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { AppProviders } from "../../app/AppProviders";
import { createQueryClient } from "../../app/queryClient";
import { routes } from "../../app/router";
import { inboxMessageFixtures } from "../../test/fixtures";
import { server } from "../../test/mocks/server";

function renderInbox(path = "/accounts/acc_primary/inbox") {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  const queryClient = createQueryClient();
  const view = render(
    <AppProviders queryClient={queryClient}>
      <RouterProvider router={router} />
    </AppProviders>,
  );
  return { ...view, queryClient, router };
}

describe("InboxPage", () => {
  it("loads the current account inbox and exposes its aliases as a URL-backed filter", async () => {
    const requests: URL[] = [];
    server.use(
      http.get("*/api/inbox", ({ request }) => {
        const url = new URL(request.url);
        requests.push(url);
        return HttpResponse.json({
          data: {
            account_id: url.searchParams.get("account_id") ?? "",
            alias: url.searchParams.get("alias") ?? "",
            count: inboxMessageFixtures.length,
            messages: inboxMessageFixtures,
            method: "imap",
          },
          success: true,
        });
      }),
    );
    const user = userEvent.setup();
    const { router } = renderInbox("/accounts/acc_primary/inbox?source=workspace");

    const aliasSelect = await screen.findByLabelText("别名");
    expect(screen.getByLabelText("账户")).toHaveValue("acc_primary");
    expect(aliasSelect).toHaveValue("");
    expect(screen.getByLabelText("时间范围")).toHaveValue("7");
    expect(screen.getByLabelText("数量")).toHaveValue("20");
    expect(
      await screen.findByRole("option", { name: "quiet-orchid@icloud.com · GitHub" }),
    ).toBeInTheDocument();
    expect(screen.getByText("3 封邮件")).toBeInTheDocument();
    expect(screen.getByLabelText("实际读取方式：IMAP")).toBeInTheDocument();
    expect(requests.at(-1)?.searchParams.get("account_id")).toBe("acc_primary");
    expect(requests.at(-1)?.searchParams.get("alias")).toBe("");
    expect(requests.at(-1)?.searchParams.get("days")).toBe("7");
    expect(requests.at(-1)?.searchParams.get("limit")).toBe("20");

    await user.selectOptions(aliasSelect, "quiet-orchid@icloud.com");

    await waitFor(() =>
      expect(requests.at(-1)?.searchParams.get("alias")).toBe("quiet-orchid@icloud.com"),
    );
    expect(aliasSelect).toHaveValue("quiet-orchid@icloud.com");
    expect(new URLSearchParams(router.state.location.search).get("alias")).toBe(
      "quiet-orchid@icloud.com",
    );
    expect(new URLSearchParams(router.state.location.search).get("source")).toBe("workspace");

    await user.selectOptions(screen.getByLabelText("时间范围"), "3");
    await user.selectOptions(screen.getByLabelText("数量"), "50");

    await waitFor(() => {
      const request = requests.at(-1);
      expect(request?.searchParams.get("days")).toBe("3");
      expect(request?.searchParams.get("limit")).toBe("50");
    });
    expect(new URLSearchParams(router.state.location.search).get("days")).toBe("3");
    expect(new URLSearchParams(router.state.location.search).get("limit")).toBe("50");

    await user.selectOptions(screen.getByLabelText("时间范围"), "7");
    await user.selectOptions(screen.getByLabelText("数量"), "20");

    expect(screen.getByLabelText("时间范围")).toHaveValue("7");
    expect(screen.getByLabelText("数量")).toHaveValue("20");
    expect(new URLSearchParams(router.state.location.search).has("days")).toBe(false);
    expect(new URLSearchParams(router.state.location.search).has("limit")).toBe(false);

    const requestsBeforeRefresh = requests.length;
    await user.click(screen.getByRole("button", { name: "刷新收件箱" }));
    await waitFor(() => expect(requests.length).toBeGreaterThan(requestsBeforeRefresh));
    expect(router.state.location.search).toBe("?source=workspace&alias=quiet-orchid%40icloud.com");
  });

  it("shows a dedicated empty state when the current filters have no messages", async () => {
    server.use(
      http.get("*/api/inbox", ({ request }) => {
        const url = new URL(request.url);
        return HttpResponse.json({
          data: {
            account_id: url.searchParams.get("account_id") ?? "",
            alias: url.searchParams.get("alias") ?? "",
            count: 0,
            messages: [],
            method: "imap",
          },
          success: true,
        });
      }),
    );
    renderInbox();

    expect(
      await screen.findByRole("heading", { level: 3, name: "暂无匹配邮件" }),
    ).toBeInTheDocument();
    expect(screen.getByText("当前筛选范围内没有邮件。")).toBeInTheDocument();
    expect(screen.queryByRole("list", { name: "邮件摘要列表" })).not.toBeInTheDocument();
  });

  it("keeps filters visible and retries an inbox fallback error on demand", async () => {
    let shouldFail = true;
    server.use(
      http.get("*/api/inbox", ({ request }) => {
        if (shouldFail) {
          return HttpResponse.json(
            { message: "读取邮件失败: Web API 回退不可用", success: false },
            { status: 502 },
          );
        }
        const url = new URL(request.url);
        return HttpResponse.json({
          data: {
            account_id: url.searchParams.get("account_id") ?? "",
            alias: url.searchParams.get("alias") ?? "",
            count: inboxMessageFixtures.length,
            messages: inboxMessageFixtures,
            method: "web_api",
          },
          success: true,
        });
      }),
    );
    const user = userEvent.setup();
    const { router } = renderInbox(
      "/accounts/acc_primary/inbox?alias=quiet-orchid%40icloud.com&days=3&limit=50",
    );

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Apple 服务错误：读取邮件失败: Web API 回退不可用");
    expect(screen.getByLabelText("别名")).toHaveValue("quiet-orchid@icloud.com");
    expect(screen.getByLabelText("时间范围")).toHaveValue("3");
    expect(screen.getByLabelText("数量")).toHaveValue("50");

    shouldFail = false;
    await user.click(screen.getByRole("button", { name: "重新加载" }));

    expect(await screen.findByRole("list", { name: "邮件摘要列表" })).toBeInTheDocument();
    expect(screen.getByLabelText("实际读取方式：Web API")).toBeInTheDocument();
    expect(router.state.location.search).toBe("?alias=quiet-orchid%40icloud.com&days=3&limit=50");
  });

  it("presents gateway timeouts as a dedicated retryable inbox error", async () => {
    server.use(
      http.get("*/api/inbox", () =>
        HttpResponse.json(
          { message: "读取邮件超时，请稍后重试。", success: false },
          { status: 504 },
        ),
      ),
    );
    renderInbox();

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("读取邮件超时");
    expect(alert).toHaveTextContent("读取邮件超时，请稍后重试。");
    expect(screen.getByRole("button", { name: "重新加载" })).toBeInTheDocument();
  });

  it("renders the actual Web API fallback method returned by the inbox endpoint", async () => {
    server.use(
      http.get("*/api/inbox", ({ request }) => {
        const url = new URL(request.url);
        return HttpResponse.json({
          data: {
            account_id: url.searchParams.get("account_id") ?? "",
            alias: url.searchParams.get("alias") ?? "",
            count: inboxMessageFixtures.length,
            messages: inboxMessageFixtures,
            method: "web_api",
          },
          success: true,
        });
      }),
    );
    renderInbox();

    expect(await screen.findByLabelText("实际读取方式：Web API")).toBeInTheDocument();
    expect(screen.queryByLabelText("实际读取方式：IMAP")).not.toBeInTheDocument();
  });

  it("shows the selected message summary and switches the preview without changing filters", async () => {
    const user = userEvent.setup();
    renderInbox();

    const messageList = await screen.findByRole("list", { name: "邮件摘要列表" });
    expect(messageList).toHaveTextContent("登录确认");
    expect(messageList).toHaveTextContent("新设备登录提醒");
    expect(screen.getByRole("region", { name: "登录确认" })).toHaveTextContent(
      "GitHub <noreply@github.com>",
    );

    await user.click(screen.getByRole("button", { name: "选择邮件 新设备登录提醒" }));

    const preview = screen.getByRole("region", { name: "新设备登录提醒" });
    expect(preview).toHaveTextContent("Apple <no_reply@email.apple.com>");
    expect(preview).toHaveTextContent("silver-field@icloud.com");
    expect(preview).toHaveTextContent("账户安全设置");
    expect(screen.getByRole("button", { name: "选择邮件 新设备登录提醒" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("switches the account route and clears an alias that belongs to the previous account", async () => {
    const user = userEvent.setup();
    const { router } = renderInbox(
      "/accounts/acc_primary/inbox?source=workspace&alias=quiet-orchid%40icloud.com&days=3&limit=50",
    );

    const accountSelect = await screen.findByLabelText("账户");
    await user.selectOptions(accountSelect, "acc_pending");

    await waitFor(() => expect(router.state.location.pathname).toBe("/accounts/acc_pending/inbox"));
    expect(router.state.location.search).toBe("?source=workspace&days=3&limit=50");
    expect(screen.getByLabelText("账户")).toHaveValue("acc_pending");
    expect(screen.getByLabelText("别名")).toHaveValue("");
    expect(screen.getByLabelText("时间范围")).toHaveValue("3");
    expect(screen.getByLabelText("数量")).toHaveValue("50");
    expect(screen.getByRole("heading", { level: 2, name: "待登录账号" })).toBeInTheDocument();
  });
});
