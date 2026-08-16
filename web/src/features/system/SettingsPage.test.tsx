import { HttpResponse, http } from "msw";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { AppProviders } from "../../app/AppProviders";
import { createQueryClient } from "../../app/queryClient";
import { routes } from "../../app/router";
import { operationLogsFixture } from "../../test/fixtures";
import { server } from "../../test/mocks/server";

function renderSettings() {
  const router = createMemoryRouter(routes, { initialEntries: ["/settings"] });
  const queryClient = createQueryClient();
  const view = render(
    <AppProviders queryClient={queryClient}>
      <RouterProvider router={router} />
    </AppProviders>,
  );
  return { ...view, queryClient, router };
}

describe("SettingsPage", () => {
  it("renders the service health and configuration availability", async () => {
    renderSettings();

    expect(await screen.findByText("icloud-hme")).toBeInTheDocument();
    expect(screen.getByText("正常")).toBeInTheDocument();
    expect(screen.getByText("dev")).toBeInTheDocument();
    expect(screen.getByText("可用")).toBeInTheDocument();
    expect(screen.getByText("服务端本地配置")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重载配置" })).toBeInTheDocument();
    expect(screen.getByText("操作日志")).toBeInTheDocument();
    expect(
      screen.getByText("保留最近 7 天的关键操作与失败记录，过期后自动清理。"),
    ).toBeInTheDocument();
    expect(screen.getByText("读取收件箱")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "刷新日志" })).toBeInTheDocument();
    expect(screen.getByText("smtp.163.com:465")).toBeInTheDocument();
    expect(screen.getByLabelText("Webhook URL")).toBeInTheDocument();
    expect(screen.getByLabelText("签名密钥")).toBeInTheDocument();
    expect(screen.getByLabelText("163 发件邮箱")).toBeInTheDocument();
    expect(screen.getByLabelText("163 邮箱授权码")).toBeInTheDocument();
  });

  it("saves 163 sender and QQ recipient settings, clears the authorization input, and sends a test message", async () => {
    const user = userEvent.setup();
    renderSettings();

    const sender = await screen.findByLabelText("163 发件邮箱");
    await user.type(sender, "sender@163.com");
    await user.type(screen.getByLabelText("QQ 收件邮箱"), "recipient@qq.com");
    await user.type(screen.getByLabelText("163 邮箱授权码"), "temporary-code");
    await user.click(screen.getByRole("checkbox", { name: /启用邮件通知/ }));
    await user.click(screen.getByRole("button", { name: "保存 163 邮箱设置" }));

    await waitFor(() => expect(screen.getByLabelText("163 邮箱授权码")).toHaveValue(""));
    const testButton = screen.getByRole("button", { name: "发送测试邮件" });
    expect(testButton).toBeEnabled();
    await user.click(testButton);
    await waitFor(async () => {
      const notifications = await screen.findAllByRole("status");
      expect(
        notifications.some((notification) =>
          notification.textContent?.includes("163 test email sent"),
        ),
      ).toBe(true);
    });
  });

  it("refreshes the operation log list independently", async () => {
    const user = userEvent.setup();
    let logRequests = 0;
    server.use(
      http.get("*/api/logs", () => {
        logRequests += 1;
        return HttpResponse.json({ data: operationLogsFixture, success: true });
      }),
    );
    renderSettings();

    await screen.findByText("批量创建别名");
    const refreshButton = screen.getByRole("button", { name: "刷新日志" });
    await user.click(refreshButton);

    await waitFor(() => expect(logRequests).toBeGreaterThanOrEqual(2));
    expect(refreshButton).toBeEnabled();
  });

  it("expands complete request parameters and the original response", async () => {
    const user = userEvent.setup();
    renderSettings();

    const operation = await screen.findByText("更新 Cookie");
    const logEntry = operation.closest("li");
    expect(logEntry).not.toBeNull();
    const entry = within(logEntry as HTMLElement);
    await user.click(entry.getByText("查看请求参数原值与原始响应"));

    const requestDetails = entry.getByRole("region", { name: "请求参数原值" });
    expect(requestDetails).toHaveTextContent("PUT");
    expect(requestDetails).toHaveTextContent("/api/accounts/demo-account/cookies");
    expect(requestDetails).toHaveTextContent("id=demo-account");
    expect(requestDetails).toHaveTextContent('{"cookies":"session=demo-cookie"}');

    const responseDetails = entry.getByRole("region", { name: "原始响应" });
    expect(within(responseDetails).getByText("失败")).toBeInTheDocument();
    expect(responseDetails).toHaveTextContent('{"success":false,"message":"Cookie 已失效"}');
  });

  it("saves webhook settings, clears the secret input, and sends a test delivery", async () => {
    const user = userEvent.setup();
    renderSettings();

    await user.type(
      await screen.findByLabelText("Webhook URL"),
      "https://hooks.example.test/icloud",
    );
    await user.type(screen.getByLabelText("签名密钥"), "temporary-secret");
    await user.click(screen.getByRole("checkbox", { name: "启用 Webhook 通知" }));
    await user.click(screen.getByRole("button", { name: "保存 Webhook 设置" }));

    await waitFor(() => expect(screen.getByLabelText("签名密钥")).toHaveValue(""));
    const testButton = screen.getByRole("button", { name: "发送测试" });
    expect(testButton).toBeEnabled();
    await user.click(testButton);
    await waitFor(async () => {
      const notifications = await screen.findAllByRole("status");
      expect(
        notifications.some((notification) =>
          notification.textContent?.includes("webhook test delivery completed"),
        ),
      ).toBe(true);
    });
  });

  it("keeps a retryable operation log error visible", async () => {
    server.use(
      http.get("*/api/logs", () =>
        HttpResponse.json({ message: "logs unavailable", success: false }, { status: 502 }),
      ),
    );
    renderSettings();

    expect(await screen.findByRole("alert")).toHaveTextContent("服务暂时不可用");
    expect(screen.getByRole("button", { name: "刷新日志" })).toBeEnabled();
  });

  it("reloads configuration, refreshes health, and announces success", async () => {
    const user = userEvent.setup();
    let healthRequests = 0;
    server.use(
      http.get("*/api/health", () => {
        healthRequests += 1;
        return HttpResponse.json({
          data: {
            config_available: true,
            service: "icloud-hme",
            status: "ok",
            version: "test-version",
          },
          success: true,
        });
      }),
    );
    renderSettings();

    await screen.findByText("test-version");
    const reloadButton = screen.getByRole("button", { name: "重载配置" });
    await user.click(reloadButton);

    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("配置已重新加载"));
    expect(healthRequests).toBeGreaterThanOrEqual(2);
    expect(reloadButton).toBeEnabled();
  });

  it("keeps a retryable health error when the service check fails", async () => {
    server.use(
      http.get("*/api/health", () =>
        HttpResponse.json({ message: "health unavailable", success: false }, { status: 502 }),
      ),
    );
    renderSettings();

    expect(await screen.findByRole("alert")).toHaveTextContent("健康检查失败");
    expect(screen.getByRole("button", { name: "重新检查" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重载配置" })).toBeEnabled();
  });

  it("surfaces a configuration reload error without losing the action", async () => {
    const user = userEvent.setup();
    server.use(
      http.post("*/api/reload", () =>
        HttpResponse.json({ message: "reload unavailable", success: false }, { status: 502 }),
      ),
    );
    renderSettings();

    await screen.findByText("icloud-hme");
    await user.click(screen.getByRole("button", { name: "重载配置" }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("服务暂时不可用"));
    expect(screen.getByRole("button", { name: "重载配置" })).toBeEnabled();
  });
});
