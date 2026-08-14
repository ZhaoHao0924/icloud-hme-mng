import { HttpResponse, http } from "msw";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

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

function useMobileViewport() {
  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockImplementation((query: string) => ({
      addEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
      matches: query === "(max-width: 760px)",
      media: query,
      onchange: null,
      removeEventListener: vi.fn(),
    })),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("InboxPage", () => {
  it("uses a list-first mobile flow and returns from a selected message", async () => {
    useMobileViewport();
    const user = userEvent.setup();
    renderInbox();

    await screen.findByRole("list", { name: "邮件摘要列表" });
    expect(screen.queryByRole("region", { name: "登录确认" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "选择邮件 登录确认" }));

    expect(screen.queryByRole("list", { name: "邮件摘要列表" })).not.toBeInTheDocument();
    expect(await screen.findByRole("region", { name: "登录确认" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "返回邮件列表" }));

    expect(await screen.findByRole("list", { name: "邮件摘要列表" })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "登录确认" })).not.toBeInTheDocument();
  });

  it("renders HTML bodies in a script-isolated frame with usable links", async () => {
    server.use(
      http.get("*/api/inbox", () =>
        HttpResponse.json({
          data: {
            account_id: "acc_primary",
            alias: "",
            count: 1,
            messages: [{ ...inboxMessageFixtures[0], preview: "" }],
            method: "imap",
          },
          success: true,
        }),
      ),
      http.get("*/api/inbox/messages/:messageId", () =>
        HttpResponse.json({
          data: {
            ...inboxMessageFixtures[0],
            body: `<style>.action { color: red; }</style><a class="action" href="https://example.test/open">打开链接</a><a href="javascript:alert(1)">危险链接</a><script>alert(1)</script>`,
            content_type: "text/html",
          },
          success: true,
        }),
      ),
    );
    renderInbox();

    const frame = await screen.findByTitle("邮件正文：登录确认");
    const srcDoc = frame.getAttribute("srcdoc") ?? "";
    expect(frame).toHaveAttribute(
      "sandbox",
      "allow-same-origin allow-popups allow-popups-to-escape-sandbox",
    );
    expect(srcDoc).toContain("https://example.test/open");
    expect(srcDoc).toContain('target="_blank"');
    expect(srcDoc).toContain(".action { color: red; }");
    expect(srcDoc).toContain("table-layout: fixed !important");
    expect(srcDoc).toContain("overflow-wrap: anywhere !important");
    expect(srcDoc).not.toContain("javascript:alert");
    expect(srcDoc).not.toContain("<script");
  });

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

    const aliasInput = await screen.findByLabelText("别名");
    expect(screen.getByLabelText("账户")).toHaveValue("primary@icloud.com");
    expect(aliasInput).toHaveValue("");
    expect(screen.getByLabelText("时间范围")).toHaveValue("7");
    expect(screen.getByLabelText("数量")).toHaveValue("20");
    expect(aliasInput).toHaveAttribute("list", "inbox-alias-options");
    await waitFor(() =>
      expect(
        document.querySelector(
          'datalist#inbox-alias-options option[value="quiet-orchid@icloud.com"]',
        ),
      ).not.toBeNull(),
    );
    expect(screen.getByText("3 封邮件")).toBeInTheDocument();
    expect(screen.getByLabelText("实际读取方式：IMAP")).toBeInTheDocument();
    expect(requests.at(-1)?.searchParams.get("account_id")).toBe("acc_primary");
    expect(requests.at(-1)?.searchParams.get("alias")).toBe("");
    expect(requests.at(-1)?.searchParams.get("days")).toBe("7");
    expect(requests.at(-1)?.searchParams.get("limit")).toBe("20");

    await user.clear(aliasInput);
    await user.type(aliasInput, "quiet-orchid@icloud.com{Enter}");

    await waitFor(() =>
      expect(requests.at(-1)?.searchParams.get("alias")).toBe("quiet-orchid@icloud.com"),
    );
    expect(aliasInput).toHaveValue("quiet-orchid@icloud.com");
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

  it("loads IMAP message bodies on demand and reuses cached previews", async () => {
    const listRequests: URL[] = [];
    const detailRequests: string[] = [];
    server.use(
      http.get("*/api/inbox", ({ request }) => {
        const url = new URL(request.url);
        listRequests.push(url);
        return HttpResponse.json({
          data: {
            account_id: "acc_primary",
            alias: "",
            count: inboxMessageFixtures.length,
            messages: inboxMessageFixtures.map((message) => ({ ...message, preview: "" })),
            method: "imap",
          },
          success: true,
        });
      }),
      http.get("*/api/inbox/messages/:messageId", ({ params }) => {
        const messageId = String(params.messageId);
        detailRequests.push(messageId);
        const message = inboxMessageFixtures.find((candidate) => candidate.id === messageId);
        return HttpResponse.json({ data: message, success: true });
      }),
    );
    const user = userEvent.setup();
    renderInbox();

    const firstPreview = await screen.findByRole("region", { name: "登录确认" });
    await waitFor(() => expect(firstPreview).toHaveTextContent("请确认你的登录操作。"));
    expect(listRequests.at(-1)?.searchParams.get("include_preview")).toBe("false");
    expect(listRequests.at(-1)?.searchParams.get("first_preview")).toBeNull();
    expect(detailRequests).toEqual(["1042"]);

    await user.click(screen.getByRole("button", { name: "选择邮件 新设备登录提醒" }));
    const secondPreview = await screen.findByRole("region", { name: "新设备登录提醒" });
    await waitFor(() => expect(secondPreview).toHaveTextContent("账户安全设置"));
    expect(detailRequests).toEqual(["1042", "1041"]);

    await user.click(screen.getByRole("button", { name: "选择邮件 登录确认" }));
    await waitFor(() =>
      expect(screen.getByRole("region", { name: "登录确认" })).toHaveTextContent(
        "请确认你的登录操作。",
      ),
    );
    expect(detailRequests).toEqual(["1042", "1041"]);
  });

  it("appends an older cursor page without replacing the current list", async () => {
    const requests: URL[] = [];
    server.use(
      http.get("*/api/inbox", ({ request }) => {
        const url = new URL(request.url);
        requests.push(url);
        const beforeUid = url.searchParams.get("before_uid");
        if (beforeUid === "1040") {
          return HttpResponse.json({
            data: {
              account_id: "acc_primary",
              alias: "",
              count: 1,
              has_more: false,
              messages: [inboxMessageFixtures[2]],
              method: "imap",
              next_cursor: "",
            },
            success: true,
          });
        }
        return HttpResponse.json({
          data: {
            account_id: "acc_primary",
            alias: "",
            count: 2,
            has_more: true,
            messages: inboxMessageFixtures.slice(0, 2),
            method: "imap",
            next_cursor: "1040",
          },
          success: true,
        });
      }),
    );
    const user = userEvent.setup();
    renderInbox();

    const list = await screen.findByRole("list", { name: "邮件摘要列表" });
    expect(list).toHaveTextContent("登录确认");
    expect(list).toHaveTextContent("新设备登录提醒");
    await user.click(screen.getByRole("button", { name: "加载更多邮件" }));

    await waitFor(() => expect(requests.at(-1)?.searchParams.get("before_uid")).toBe("1040"));
    await waitFor(() => expect(list).toHaveTextContent("问题状态已更新"));
    expect(screen.queryByRole("button", { name: "加载更多邮件" })).not.toBeInTheDocument();
    expect(screen.getByText("3 封邮件")).toBeInTheDocument();
  });

  it("uses an IMAP preview returned with the list without a detail request", async () => {
    let detailRequests = 0;
    server.use(
      http.get("*/api/inbox", () =>
        HttpResponse.json({
          data: {
            account_id: "acc_primary",
            alias: "",
            count: inboxMessageFixtures.length,
            messages: inboxMessageFixtures,
            method: "imap",
          },
          success: true,
        }),
      ),
      http.get("*/api/inbox/messages/:messageId", () => {
        detailRequests += 1;
        return HttpResponse.json({ data: inboxMessageFixtures[0], success: true });
      }),
    );

    renderInbox();

    const preview = await screen.findByRole("region", { name: "登录确认" });
    await waitFor(() => expect(preview).toHaveTextContent("请确认你的登录操作。"));
    expect(detailRequests).toBe(0);
  });

  it("keeps the inbox usable when one on-demand preview times out and retries locally", async () => {
    let detailShouldFail = true;
    server.use(
      http.get("*/api/inbox", () =>
        HttpResponse.json({
          data: {
            account_id: "acc_primary",
            alias: "",
            count: inboxMessageFixtures.length,
            messages: inboxMessageFixtures.map((message) => ({ ...message, preview: "" })),
            method: "imap",
          },
          success: true,
        }),
      ),
      http.get("*/api/inbox/messages/:messageId", ({ params }) => {
        if (detailShouldFail) {
          return HttpResponse.json(
            { message: "读取邮件超时，请稍后重试。", success: false },
            { status: 504 },
          );
        }
        const message = inboxMessageFixtures.find(
          (candidate) => candidate.id === String(params.messageId),
        );
        return HttpResponse.json({ data: message, success: true });
      }),
    );
    const user = userEvent.setup();
    renderInbox();

    const preview = await screen.findByRole("region", { name: "登录确认" });
    expect(await within(preview).findByRole("alert")).toHaveTextContent("Apple 服务暂时不可用");
    expect(screen.getByRole("list", { name: "邮件摘要列表" })).toBeInTheDocument();

    detailShouldFail = false;
    await user.click(within(preview).getByRole("button", { name: "重新加载" }));

    await waitFor(() => expect(preview).toHaveTextContent("请确认你的登录操作。"));
    expect(within(preview).queryByRole("alert")).not.toBeInTheDocument();
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

  it("keeps a manually entered alias even when it is not in the alias list", async () => {
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
            method: "web_api",
          },
          success: true,
        });
      }),
    );
    const { router } = renderInbox(
      "/accounts/acc_primary/inbox?source=workspace&alias=removed-alias%40icloud.com",
    );

    await screen.findByRole("list", { name: "邮件摘要列表" });
    expect(new URLSearchParams(router.state.location.search).get("alias")).toBe(
      "removed-alias@icloud.com",
    );
    expect(router.state.location.search).toBe("?source=workspace&alias=removed-alias%40icloud.com");
    expect(screen.getByLabelText("别名")).toHaveValue("removed-alias@icloud.com");
    expect(requests).toHaveLength(1);
    expect(requests[0]?.searchParams.get("alias")).toBe("removed-alias@icloud.com");
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
    expect(alert).toHaveTextContent("Apple 服务暂时不可用");
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
    expect(alert).toHaveTextContent("Apple 服务暂时不可用");
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

    const accountInput = await screen.findByLabelText("账户");
    await user.clear(accountInput);
    await user.type(accountInput, "acc_pending{Enter}");

    await waitFor(() => expect(router.state.location.pathname).toBe("/accounts/acc_pending/inbox"));
    expect(router.state.location.search).toBe("?source=workspace&days=3&limit=50");
    expect(screen.getByLabelText("账户")).toHaveValue("pending@icloud.com.cn");
    expect(screen.getByLabelText("别名")).toHaveValue("");
    expect(screen.getByLabelText("时间范围")).toHaveValue("3");
    expect(screen.getByLabelText("数量")).toHaveValue("50");
    expect(screen.getByRole("heading", { level: 2, name: "待登录账号" })).toBeInTheDocument();
  });
});
