import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { queryKeys } from "../../api/queries";
import { AppProviders } from "../../app/AppProviders";
import { createQueryClient } from "../../app/queryClient";
import { routes } from "../../app/router";
import { mockScenario, server } from "../../test/mocks/server";
import { createSessionRecoveryLocationState } from "./sessionRecoveryState";

function renderSecurityPage(recoveryFrom?: string) {
  const queryClient = createQueryClient();
  const router = createMemoryRouter(routes, {
    initialEntries: [
      recoveryFrom
        ? {
            pathname: "/accounts/acc_pending/security",
            state: createSessionRecoveryLocationState(recoveryFrom),
          }
        : "/accounts/acc_pending/security",
    ],
  });
  render(
    <AppProviders queryClient={queryClient}>
      <RouterProvider router={router} />
    </AppProviders>,
  );
  return { queryClient, router };
}

describe("SecurityPage Cookie form", () => {
  it("keeps invalid input available for correction without sending it", async () => {
    const user = userEvent.setup();
    const { queryClient } = renderSecurityPage();
    const section = await screen.findByRole("region", { name: "Cookie" }, { timeout: 3_000 });
    const input = within(section).getByLabelText("Cookie 数据");

    await user.click(input);
    await user.paste("{invalid");
    await user.click(within(section).getByRole("button", { name: "更新 Cookie" }));

    expect(within(section).getByText("Cookie JSON 格式无效")).toBeInTheDocument();
    expect(input).toHaveValue("{invalid");
    expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
  });

  it("updates Cookie state while keeping secrets out of query and mutation caches", async () => {
    const user = userEvent.setup();
    const { queryClient } = renderSecurityPage();
    const section = await screen.findByRole("region", { name: "Cookie" });
    const input = within(section).getByLabelText("Cookie 数据");

    expect(within(section).getByText("未配置")).toBeInTheDocument();
    expect(input).toHaveClass("secret-input-concealed");
    await user.click(within(section).getByRole("button", { name: "显示 Cookie" }));
    expect(input).not.toHaveClass("secret-input-concealed");

    await user.type(input, "session=token-value; user=owner");
    await user.click(within(section).getByRole("button", { name: "更新 Cookie" }));

    expect(input).toHaveValue("");
    expect(input).toHaveClass("secret-input-concealed");
    expect(await screen.findByRole("status")).toHaveTextContent("Cookie 已更新待登录账号");
    await waitFor(() => expect(within(section).getByText("已配置")).toBeInTheDocument());
    expect(screen.getAllByText("正常").length).toBeGreaterThan(0);

    const mutation = queryClient.getMutationCache().getAll().at(-1);
    expect(mutation?.state.variables).toBeUndefined();
    expect(JSON.stringify(mutation?.state)).not.toContain("token-value");
    expect(JSON.stringify(queryClient.getQueryData(["accounts"]))).not.toContain("token-value");
    expect(document.body).not.toHaveTextContent("token-value");
  });

  it("clears submitted Cookie data when the server rejects the update", async () => {
    const user = userEvent.setup();
    const { queryClient } = renderSecurityPage();
    const section = await screen.findByRole("region", { name: "Cookie" });
    const input = within(section).getByLabelText("Cookie 数据");

    mockScenario.set("error");
    await user.type(input, "session=retry-value");
    await user.click(within(section).getByRole("button", { name: "更新 Cookie" }));

    expect(await within(section).findByRole("alert")).toHaveTextContent("Apple 服务错误");
    expect(input).toHaveValue("");
    expect(within(section).getByText("未配置")).toBeInTheDocument();
    const mutation = queryClient.getMutationCache().getAll().at(-1);
    expect(mutation?.state.variables).toBeUndefined();
    expect(JSON.stringify(mutation?.state)).not.toContain("retry-value");
    expect(document.body).not.toHaveTextContent("retry-value");
  });
});

describe("SecurityPage App Password form", () => {
  it("prefills the account email and validates fields before sending", async () => {
    const user = userEvent.setup();
    const { queryClient } = renderSecurityPage();
    const section = await screen.findByRole("region", { name: "App 专用密码" });
    const emailInput = within(section).getByLabelText("iCloud 邮箱");
    const passwordInput = within(section).getByLabelText("App 专用密码");

    expect(emailInput).toHaveValue("pending@icloud.com.cn");
    expect(passwordInput).toHaveAttribute("type", "password");
    await user.clear(emailInput);
    await user.type(emailInput, "not-an-email");
    await user.click(within(section).getByRole("button", { name: "验证并保存" }));

    expect(within(section).getByText("请输入有效的 iCloud 邮箱")).toBeInTheDocument();
    expect(within(section).getByText("请输入 App 专用密码")).toBeInTheDocument();
    expect(passwordInput).toHaveValue("");
    expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
  });

  it("validates and stores the capability without retaining the password", async () => {
    const user = userEvent.setup();
    const { queryClient } = renderSecurityPage();
    const section = await screen.findByRole("region", { name: "App 专用密码" });
    const emailInput = within(section).getByLabelText("iCloud 邮箱");
    const passwordInput = within(section).getByLabelText("App 专用密码");

    expect(within(section).getByText("未配置")).toBeInTheDocument();
    await user.click(within(section).getByRole("button", { name: "显示 App 专用密码" }));
    expect(passwordInput).toHaveAttribute("type", "text");
    await user.type(passwordInput, "abcd-efgh-ijkl-mnop");
    await user.click(within(section).getByRole("button", { name: "验证并保存" }));

    await waitFor(() => expect(passwordInput).toHaveValue(""));
    expect(passwordInput).toHaveAttribute("type", "password");
    expect(emailInput).toHaveValue("pending@icloud.com.cn");
    expect(await screen.findByRole("status")).toHaveTextContent("App 密码已验证待登录账号");
    await waitFor(() => expect(within(section).getByText("已配置")).toBeInTheDocument());

    const mutation = queryClient.getMutationCache().getAll().at(-1);
    expect(mutation?.state.variables).toBeUndefined();
    expect(JSON.stringify(mutation?.state)).not.toContain("abcd-efgh-ijkl-mnop");
    expect(JSON.stringify(queryClient.getQueryData(["accounts"]))).not.toContain(
      "abcd-efgh-ijkl-mnop",
    );
    expect(document.body).not.toHaveTextContent("abcd-efgh-ijkl-mnop");
  });

  it("clears the submitted password when connection validation fails", async () => {
    const user = userEvent.setup();
    const { queryClient } = renderSecurityPage();
    const section = await screen.findByRole("region", { name: "App 专用密码" });
    const emailInput = within(section).getByLabelText("iCloud 邮箱");
    const passwordInput = within(section).getByLabelText("App 专用密码");

    mockScenario.set("error");
    await user.type(passwordInput, "qrst-uvwx-yzab-cdef");
    await user.click(within(section).getByRole("button", { name: "验证并保存" }));

    expect(await within(section).findByRole("alert")).toHaveTextContent("Apple 服务错误");
    expect(emailInput).toHaveValue("pending@icloud.com.cn");
    expect(passwordInput).toHaveValue("");
    expect(within(section).getByText("未配置")).toBeInTheDocument();
    const mutation = queryClient.getMutationCache().getAll().at(-1);
    expect(mutation?.state.variables).toBeUndefined();
    expect(JSON.stringify(mutation?.state)).not.toContain("qrst-uvwx-yzab-cdef");
    expect(document.body).not.toHaveTextContent("qrst-uvwx-yzab-cdef");
  });
});

describe("SecurityPage Apple login form", () => {
  it("validates the password without creating a mutation", async () => {
    const user = userEvent.setup();
    const { queryClient } = renderSecurityPage();
    const section = await screen.findByRole("region", { name: "Apple 登录" });
    const passwordInput = within(section).getByLabelText("Apple ID 密码");

    expect(within(section).getByText("Cookie 未配置")).toBeInTheDocument();
    expect(passwordInput).toHaveAttribute("type", "password");
    await user.click(within(section).getByRole("button", { name: "登录" }));

    expect(within(section).getByText("请输入 Apple ID 密码")).toBeInTheDocument();
    expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
  });

  it("completes a direct login without retaining the password", async () => {
    const user = userEvent.setup();
    const { queryClient } = renderSecurityPage();
    const section = await screen.findByRole("region", { name: "Apple 登录" });
    const passwordInput = within(section).getByLabelText("Apple ID 密码");

    await user.click(within(section).getByRole("button", { name: "显示 Apple ID 密码" }));
    expect(passwordInput).toHaveAttribute("type", "text");
    await user.type(passwordInput, "direct-login-secret");
    await user.click(within(section).getByRole("button", { name: "登录" }));

    await waitFor(() => expect(passwordInput).toHaveValue(""));
    expect(passwordInput).toHaveAttribute("type", "password");
    expect(await screen.findByRole("status")).toHaveTextContent("Apple 登录成功待登录账号");
    await waitFor(() => expect(within(section).getByText("Cookie 已配置")).toBeInTheDocument());
    expect(screen.getAllByText("正常").length).toBeGreaterThan(0);

    const mutation = queryClient.getMutationCache().getAll().at(-1);
    expect(mutation?.state.variables).toBeUndefined();
    expect(JSON.stringify(mutation?.state)).not.toContain("direct-login-secret");
    expect(JSON.stringify(queryClient.getQueryData(["accounts"]))).not.toContain(
      "direct-login-secret",
    );
    expect(document.body).not.toHaveTextContent("direct-login-secret");
  });

  it("returns to the source page after a recovery login", async () => {
    const user = userEvent.setup();
    const { router } = renderSecurityPage("/accounts/acc_pending/inbox");
    const recoveryAlert = await screen.findByRole("alert");
    expect(recoveryAlert).toHaveTextContent("Cookie 会话已过期");
    const cookieInput = screen.getByLabelText("Cookie 数据");
    await waitFor(() => expect(cookieInput).toHaveFocus());
    const section = screen.getByRole("region", { name: "Apple 登录" });
    const passwordInput = within(section).getByLabelText("Apple ID 密码");

    await user.type(passwordInput, "recovery-login-secret");
    await user.click(within(section).getByRole("button", { name: "登录" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/accounts/acc_pending/inbox"));
    expect(router.state.location.state).toBeNull();
    expect(document.body).not.toHaveTextContent("recovery-login-secret");
  });

  it("opens a focused OTP dialog without retaining the password or challenge ID", async () => {
    const user = userEvent.setup();
    const { queryClient } = renderSecurityPage();
    const section = await screen.findByRole("region", { name: "Apple 登录" });
    const passwordInput = within(section).getByLabelText("Apple ID 密码");

    mockScenario.set("otp");
    await user.type(passwordInput, "otp-login-secret");
    await user.click(within(section).getByRole("button", { name: "登录" }));

    const dialog = await screen.findByRole("dialog", { name: "验证 Apple 登录" });
    const otpInput = within(dialog).getByLabelText("6 位验证码");

    expect(otpInput).toHaveFocus();
    expect(within(dialog).getByText("双重认证 · 5:00 后过期")).toBeInTheDocument();
    expect(passwordInput).toHaveValue("");
    expect(within(section).getByText("Cookie 未配置")).toBeInTheDocument();
    const mutationState = JSON.stringify(
      queryClient
        .getMutationCache()
        .getAll()
        .map((mutation) => mutation.state),
    );
    expect(mutationState).not.toContain("otp-login-secret");
    expect(mutationState).not.toContain("mock-login-challenge");
    expect(document.body).not.toHaveTextContent("otp-login-secret");
  });

  it("rejects empty and malformed OTP values without sending a verify request", async () => {
    const user = userEvent.setup();
    let verifyRequests = 0;
    server.use(
      http.post("*/api/accounts/:accountId/login/verify", () => {
        verifyRequests += 1;
        return HttpResponse.json({ success: false }, { status: 500 });
      }),
    );
    renderSecurityPage();
    const section = await screen.findByRole("region", { name: "Apple 登录" });

    mockScenario.set("otp");
    await user.type(within(section).getByLabelText("Apple ID 密码"), "otp-validation-secret");
    await user.click(within(section).getByRole("button", { name: "登录" }));
    const dialog = await screen.findByRole("dialog", { name: "验证 Apple 登录" });
    const otpInput = within(dialog).getByLabelText("6 位验证码");
    const verifyButton = within(dialog).getByRole("button", { name: "验证" });

    await user.click(verifyButton);
    expect(within(dialog).getByText("请输入 6 位数字验证码")).toBeInTheDocument();
    await user.type(otpInput, "12a456");
    await user.click(verifyButton);

    expect(within(dialog).getByText("请输入 6 位数字验证码")).toBeInTheDocument();
    expect(verifyRequests).toBe(0);
  });

  it("verifies OTP, refreshes only accounts, and clears all submitted secrets", async () => {
    const user = userEvent.setup();
    const { queryClient, router } = renderSecurityPage();
    const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");
    const section = await screen.findByRole("region", { name: "Apple 登录" });
    const passwordInput = within(section).getByLabelText("Apple ID 密码");

    mockScenario.set("otp");
    await user.type(passwordInput, "otp-success-secret");
    await user.click(within(section).getByRole("button", { name: "登录" }));
    const dialog = await screen.findByRole("dialog", { name: "验证 Apple 登录" });
    const otpInput = within(dialog).getByLabelText("6 位验证码");
    await user.type(otpInput, "123456");
    await user.click(within(dialog).getByRole("button", { name: "验证" }));

    expect(otpInput).toHaveValue("");
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "验证 Apple 登录" })).not.toBeInTheDocument(),
    );
    expect(await screen.findByRole("status")).toHaveTextContent("Apple 登录成功待登录账号");
    await waitFor(() => expect(within(section).getByText("Cookie 已配置")).toBeInTheDocument());
    expect(screen.getAllByText("正常").length).toBeGreaterThan(0);
    expect(invalidateQueries).toHaveBeenCalledTimes(1);
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: queryKeys.accounts });

    const clientState = JSON.stringify({
      mutations: queryClient
        .getMutationCache()
        .getAll()
        .map((mutation) => mutation.state),
      queries: queryClient.getQueryData(queryKeys.accounts),
    });
    expect(clientState).not.toContain("otp-success-secret");
    expect(clientState).not.toContain("123456");
    expect(clientState).not.toContain("mock-login-challenge");
    expect(document.body).not.toHaveTextContent("otp-success-secret");
    expect(document.body).not.toHaveTextContent("123456");
    expect(router.state.location.pathname).toBe("/accounts/acc_pending/security");
    expect(router.state.location.search).toBe("");
  });

  it("closes a consumed challenge after a 401 and requires a new password login", async () => {
    const user = userEvent.setup();
    const { queryClient } = renderSecurityPage();
    const section = await screen.findByRole("region", { name: "Apple 登录" });
    const passwordInput = within(section).getByLabelText("Apple ID 密码");

    mockScenario.set("otp");
    await user.type(passwordInput, "otp-retry-secret");
    await user.click(within(section).getByRole("button", { name: "登录" }));
    const dialog = await screen.findByRole("dialog", { name: "验证 Apple 登录" });
    await user.type(within(dialog).getByLabelText("6 位验证码"), "654321");
    await user.click(within(dialog).getByRole("button", { name: "验证" }));

    expect(await within(section).findByRole("alert")).toHaveTextContent(
      "验证码验证失败: 双重认证验证码无效",
    );
    expect(screen.queryByRole("dialog", { name: "验证 Apple 登录" })).not.toBeInTheDocument();
    expect(passwordInput).toHaveValue("");
    expect(within(section).getByText("Cookie 未配置")).toBeInTheDocument();

    const mutationState = JSON.stringify(
      queryClient
        .getMutationCache()
        .getAll()
        .map((mutation) => mutation.state),
    );
    expect(mutationState).not.toContain("otp-retry-secret");
    expect(mutationState).not.toContain("654321");
    expect(mutationState).not.toContain("mock-login-challenge");

    await user.type(passwordInput, "fresh-login-secret");
    await user.click(within(section).getByRole("button", { name: "登录" }));
    expect(await screen.findByRole("dialog", { name: "验证 Apple 登录" })).toBeInTheDocument();
  });

  it("surfaces a 410 challenge error and returns to password login", async () => {
    const user = userEvent.setup();
    server.use(
      http.post("*/api/accounts/:accountId/login/verify", () =>
        HttpResponse.json(
          { message: "登录 challenge 无效或已过期，请重新提交密码", success: false },
          { status: 410 },
        ),
      ),
    );
    renderSecurityPage();
    const section = await screen.findByRole("region", { name: "Apple 登录" });

    mockScenario.set("otp");
    await user.type(within(section).getByLabelText("Apple ID 密码"), "expired-server-secret");
    await user.click(within(section).getByRole("button", { name: "登录" }));
    const dialog = await screen.findByRole("dialog", { name: "验证 Apple 登录" });
    await user.type(within(dialog).getByLabelText("6 位验证码"), "123456");
    await user.click(within(dialog).getByRole("button", { name: "验证" }));

    expect(await within(section).findByRole("alert")).toHaveTextContent(
      "登录 challenge 无效或已过期，请重新提交密码",
    );
    expect(screen.queryByRole("dialog", { name: "验证 Apple 登录" })).not.toBeInTheDocument();
    expect(within(section).getByLabelText("Apple ID 密码")).toBeEnabled();
  });

  it("expires a challenge locally and asks for the password again", async () => {
    const user = userEvent.setup();
    server.use(
      http.post("*/api/accounts/:accountId/login/start", () =>
        HttpResponse.json({
          data: {
            challenge_id: "short-lived-challenge",
            expires_in: 1,
            status: "otp_required",
          },
          success: true,
        }),
      ),
    );
    renderSecurityPage();
    const section = await screen.findByRole("region", { name: "Apple 登录" });

    await user.type(within(section).getByLabelText("Apple ID 密码"), "short-lived-secret");
    await user.click(within(section).getByRole("button", { name: "登录" }));
    expect(await screen.findByRole("dialog", { name: "验证 Apple 登录" })).toBeInTheDocument();

    await waitFor(
      () =>
        expect(screen.queryByRole("dialog", { name: "验证 Apple 登录" })).not.toBeInTheDocument(),
      { timeout: 2_500 },
    );
    expect(within(section).getByRole("status")).toHaveTextContent(
      "验证码已过期，请重新提交 Apple ID 密码。",
    );
  });

  it("cancels an OTP challenge and returns to password login", async () => {
    const user = userEvent.setup();
    renderSecurityPage();
    const section = await screen.findByRole("region", { name: "Apple 登录" });

    mockScenario.set("otp");
    await user.type(within(section).getByLabelText("Apple ID 密码"), "cancel-login-secret");
    await user.click(within(section).getByRole("button", { name: "登录" }));
    const dialog = await screen.findByRole("dialog", { name: "验证 Apple 登录" });
    await user.click(within(dialog).getByRole("button", { name: "取消" }));

    expect(screen.queryByRole("dialog", { name: "验证 Apple 登录" })).not.toBeInTheDocument();
    expect(within(section).getByRole("status")).toHaveTextContent("验证码验证已取消。");
    expect(within(section).getByLabelText("Apple ID 密码")).toBeEnabled();
  });

  it("shows login-specific 401 errors and clears the rejected password", async () => {
    const user = userEvent.setup();
    const { queryClient } = renderSecurityPage();
    const section = await screen.findByRole("region", { name: "Apple 登录" });
    const passwordInput = within(section).getByLabelText("Apple ID 密码");

    server.use(
      http.post("*/api/accounts/:accountId/login/start", () =>
        HttpResponse.json(
          { message: "登录失败：Apple ID 或密码错误", success: false },
          { status: 401 },
        ),
      ),
    );
    await user.type(passwordInput, "rejected-login-secret");
    await user.click(within(section).getByRole("button", { name: "登录" }));

    expect(await within(section).findByRole("alert")).toHaveTextContent(
      "登录失败：Apple ID 或密码错误",
    );
    expect(within(section).queryByText("会话已过期，请更新 Cookie。")).not.toBeInTheDocument();
    expect(passwordInput).toHaveValue("");
    expect(within(section).getByText("Cookie 未配置")).toBeInTheDocument();
    const mutation = queryClient.getMutationCache().getAll().at(-1);
    expect(mutation?.state.variables).toBeUndefined();
    expect(JSON.stringify(mutation?.state)).not.toContain("rejected-login-secret");
    expect(document.body).not.toHaveTextContent("rejected-login-secret");
  });
});
