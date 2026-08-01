import { describe, expect, it, vi } from "vitest";

import {
  ApiError,
  createApiClient,
  getApiErrorMessage,
  isApiTokenError,
  isSessionExpiredError,
  shouldRetryApiRequest,
} from "./client";

const accountFixture = {
  alias_active: 2,
  alias_total: 3,
  created_at: "2026-08-01T10:00:00+08:00",
  has_app_password: true,
  has_cookies: true,
  host: "icloud.com",
  icloud_email: "owner@icloud.com",
  id: "acc_main",
  last_error: "",
  last_validated: "2026-08-01T10:00:00+08:00",
  name: "主账号",
  proxy_configured: false,
  real_email: "owner@example.com",
  status: "active",
};

function jsonResponse(payload: unknown, status = 200) {
  return {
    json: async () => payload,
    ok: status >= 200 && status < 300,
    status,
  } as Response;
}

describe("API client", () => {
  it("validates account DTOs and strips unexpected response fields", async () => {
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          data: [
            {
              ...accountFixture,
              app_password: "must-not-reach-ui-state",
              cookies: { "X-APPLE-WEBAUTH-TOKEN": "must-not-reach-ui-state" },
            },
          ],
          success: true,
        }),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    const accounts = await client.listAccounts();

    expect(fetcher).toHaveBeenCalledWith(
      "/api/accounts",
      expect.objectContaining({ cache: "no-store", method: "GET" }),
    );
    expect(accounts).toEqual([accountFixture]);
    expect(accounts[0]).not.toHaveProperty("app_password");
    expect(accounts[0]).not.toHaveProperty("cookies");
  });

  it("serializes mutation data in the JSON body and encodes dynamic path segments", async () => {
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          data: accountFixture,
          success: true,
        }),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await client.setAppPassword("account / id", {
      appPassword: "abcd-efgh-ijkl-mnop",
      icloudEmail: "owner@icloud.com",
    });

    expect(fetcher).toHaveBeenCalledWith(
      "/api/accounts/account%20%2F%20id/password",
      expect.objectContaining({
        body: JSON.stringify({
          app_password: "abcd-efgh-ijkl-mnop",
          icloud_email: "owner@icloud.com",
        }),
        headers: {
          Accept: "application/json",
          "Content-Type": "application/json",
        },
        method: "POST",
      }),
    );
  });

  it("adds an in-memory API token only to the Authorization header", async () => {
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          data: [],
          success: true,
        }),
      ),
    );
    const client = createApiClient({
      apiTokenProvider: () => "fe306-client-token",
      fetch: fetcher as unknown as typeof fetch,
    });

    await client.listAccounts();

    expect(fetcher).toHaveBeenCalledWith(
      "/api/accounts",
      expect.objectContaining({
        headers: {
          Accept: "application/json",
          Authorization: "Bearer fe306-client-token",
        },
        method: "GET",
      }),
    );
  });

  it("serializes alias creation through the non-idempotent create endpoint", async () => {
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          data: {
            account_id: "acc_main",
            created_at: "2026-08-01T09:00:00Z",
            email: "new-alias@icloud.com",
            label: "新闻订阅",
          },
          success: true,
        }),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await expect(
      client.createAlias({ accountId: "acc_main", label: "新闻订阅" }),
    ).resolves.toMatchObject({ email: "new-alias@icloud.com" });
    expect(fetcher).toHaveBeenCalledWith(
      "/api/create",
      expect.objectContaining({
        body: JSON.stringify({ account_id: "acc_main", label: "新闻订阅" }),
        method: "POST",
      }),
    );
  });

  it("sends alias status actions to encoded endpoints with the account body", async () => {
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          data: { anonymous_id: "alias / id", success: true },
          success: true,
        }),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await client.deactivateAlias("account / id", "alias / id");
    await client.reactivateAlias("account / id", "alias / id");

    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      "/api/aliases/alias%20%2F%20id/deactivate",
      expect.objectContaining({
        body: JSON.stringify({ account_id: "account / id" }),
        method: "POST",
      }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      "/api/aliases/alias%20%2F%20id/reactivate",
      expect.objectContaining({
        body: JSON.stringify({ account_id: "account / id" }),
        method: "POST",
      }),
    );
  });

  it("sends alias deletion to an encoded endpoint with the account body", async () => {
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          data: { anonymous_id: "alias / id" },
          success: true,
        }),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await client.deleteAlias("account / id", "alias / id");

    expect(fetcher).toHaveBeenCalledWith(
      "/api/aliases/alias%20%2F%20id",
      expect.objectContaining({
        body: JSON.stringify({ account_id: "account / id" }),
        method: "DELETE",
      }),
    );
  });

  it("serializes Cookie maps only in the update request body", async () => {
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          data: accountFixture,
          success: true,
        }),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await client.updateCookies("account / id", { session: "token-value", user: "owner" });

    expect(fetcher).toHaveBeenCalledWith(
      "/api/accounts/account%20%2F%20id/cookies",
      expect.objectContaining({
        body: JSON.stringify({ cookies: { session: "token-value", user: "owner" } }),
        method: "PUT",
      }),
    );
  });

  it("maps unauthorized API errors to the session-expired state without retrying", async () => {
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse(
          {
            message: "iCloud 会话失效，请更新 Cookie",
            success: false,
          },
          401,
        ),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    let caught: unknown;
    try {
      await client.listAliases("acc_main");
    } catch (error) {
      caught = error;
    }

    expect(caught).toBeInstanceOf(ApiError);
    expect(isSessionExpiredError(caught)).toBe(true);
    expect(getApiErrorMessage(caught)).toBe("会话已过期，请更新 Cookie。");
    expect(shouldRetryApiRequest(0, caught)).toBe(false);
  });

  it("keeps API token rejections out of the iCloud session recovery path", async () => {
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse(
          {
            code: "api_token_invalid",
            message: "API 访问令牌无效或缺失",
            success: false,
          },
          401,
        ),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await expect(client.listAccounts()).rejects.toMatchObject({
      code: "api_token_invalid",
      status: 401,
    });

    try {
      await client.listAccounts();
    } catch (error) {
      expect(isApiTokenError(error)).toBe(true);
      expect(isSessionExpiredError(error)).toBe(false);
      expect(getApiErrorMessage(error)).toBe("需要 API 访问令牌，请输入后继续。");
    }
  });

  it("retains the cleaned backend summary for Apple service errors", () => {
    const error = new ApiError({
      kind: "http",
      message: "读取邮件失败: upstream unavailable",
      status: 502,
    });

    expect(getApiErrorMessage(error)).toBe("Apple 服务错误：读取邮件失败: upstream unavailable");
    expect(shouldRetryApiRequest(0, error)).toBe(false);
  });

  it("rejects malformed successful responses as a contract error", async () => {
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          data: [{ id: "acc_main" }],
          success: true,
        }),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await expect(client.listAccounts()).rejects.toMatchObject({
      kind: "invalid_response",
      status: 200,
    });
  });

  it("forwards aborts without converting them into a network error", async () => {
    const abortError = new DOMException("Aborted", "AbortError");
    const fetcher = vi.fn(() => Promise.reject(abortError));
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });
    const controller = new AbortController();

    await expect(client.listAccounts({ signal: controller.signal })).rejects.toBe(abortError);
    expect(fetcher).toHaveBeenCalledWith(
      "/api/accounts",
      expect.objectContaining({ signal: controller.signal }),
    );
  });

  it("maps an inbox request timeout to a manual-retry error", async () => {
    const fetcher = vi.fn(
      (_url: string, request?: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          request?.signal?.addEventListener(
            "abort",
            () => reject(new DOMException("Aborted", "AbortError")),
            { once: true },
          );
        }),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await expect(
      client.listInbox({ accountId: "acc_main" }, { timeoutMs: 1 }),
    ).rejects.toMatchObject({
      kind: "timeout",
      message: "读取邮件超时，请稍后重试。",
    } satisfies Partial<ApiError>);
    expect(shouldRetryApiRequest(0, new ApiError({ kind: "timeout", message: "超时" }))).toBe(
      false,
    );
  });
});
