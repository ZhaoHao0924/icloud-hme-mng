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

const aliasAutomationFixture = {
  enabled: true,
  interval_minutes: 60,
  allowed_weekdays: [],
  execution_window_start: "",
  execution_window_end: "",
  label_prefix: "自动补充",
  last_active: 6,
  last_created: 2,
  last_error: "",
  last_run_at: "2026-08-01T09:00:00Z",
  last_status: "success",
  max_batch_size: 5,
  max_total_aliases: 800,
  max_failure_count: 3,
  daily_creation_limit: 20,
  minimum_active: 4,
  next_run_at: "2026-08-01T10:00:00Z",
  scheduled_batch_size: 2,
  target_active: 6,
  target_created: 750,
  created_total: 2,
  consecutive_failure: 0,
  pause_reason: "",
  daily_created: 2,
  daily_created_date: "2026-08-01",
};

function jsonResponse(payload: unknown, status = 200) {
  return {
    json: async () => payload,
    ok: status >= 200 && status < 300,
    status,
  } as Response;
}

function textResponse(body: string, status = 200) {
  return {
    json: async () => JSON.parse(body),
    ok: status >= 200 && status < 300,
    status,
    text: async () => body,
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

  it("loads privacy-safe operation log entries and strips unexpected fields", async () => {
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          data: {
            count: 1,
            entries: [
              {
                duration_ms: 842,
                level: "info",
                operation: "读取收件箱",
                request_body: "must-not-reach-ui-state",
                status: 200,
                timestamp: "2026-08-02T08:30:00Z",
              },
            ],
            retention_days: 7,
          },
          success: true,
        }),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    const logs = await client.listOperationLogs();

    expect(fetcher).toHaveBeenCalledWith(
      "/api/logs?limit=200",
      expect.objectContaining({ cache: "no-store", method: "GET" }),
    );
    expect(logs).toEqual({
      count: 1,
      entries: [
        {
          duration_ms: 842,
          level: "info",
          operation: "读取收件箱",
          status: 200,
          timestamp: "2026-08-02T08:30:00Z",
        },
      ],
      retention_days: 7,
    });
    expect(logs.entries[0]).not.toHaveProperty("request_body");
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

  it("serializes automation rules and validates the returned schedule", async () => {
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          data: aliasAutomationFixture,
          success: true,
        }),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await expect(
      client.updateAliasAutomation("account / id", {
        enabled: true,
        intervalMinutes: 60,
        allowedWeekdays: [1, 3, 5],
        executionWindowStart: "09:00",
        executionWindowEnd: "17:00",
        labelPrefix: "自动补充",
        maxBatchSize: 5,
        maxTotalAliases: 800,
        maxFailureCount: 3,
        dailyCreationLimit: 20,
        minimumActive: 4,
        scheduledBatchSize: 2,
        targetActive: 6,
        targetCreated: 750,
      }),
    ).resolves.toEqual(aliasAutomationFixture);
    expect(fetcher).toHaveBeenCalledWith(
      "/api/accounts/account%20%2F%20id/alias-automation",
      expect.objectContaining({ method: "PUT" }),
    );
    const [, requestOptions] =
      (fetcher.mock.calls as unknown as Array<[string, RequestInit]>)[0] ?? [];
    expect(JSON.parse(String(requestOptions?.body))).toEqual({
      enabled: true,
      interval_minutes: 60,
      allowed_weekdays: [1, 3, 5],
      execution_window_start: "09:00",
      execution_window_end: "17:00",
      label_prefix: "自动补充",
      max_batch_size: 5,
      max_total_aliases: 800,
      max_failure_count: 3,
      daily_creation_limit: 20,
      minimum_active: 4,
      scheduled_batch_size: 2,
      target_active: 6,
      target_created: 750,
    });
  });

  it("requests a read-only automation preview from the account-scoped endpoint", async () => {
    const preview = {
      account_id: "account / id",
      active_aliases: 2,
      automation: aliasAutomationFixture,
      daily_remaining: 18,
      max_total_aliases: 800,
      next_eligible_at: "",
      remaining_total_capacity: 797,
      requested: 2,
      schedule_allowed: true,
      schedule_reason: "",
      target_remaining: 748,
      total_aliases: 3,
    };
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          data: preview,
          success: true,
        }),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await expect(client.previewAliasAutomation("account / id")).resolves.toEqual(preview);
    expect(fetcher).toHaveBeenCalledWith(
      "/api/accounts/account%20%2F%20id/alias-automation/preview",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("retrieves retained creation history and CSV exports from account-scoped endpoints", async () => {
    const history = {
      account_id: "account / id",
      count: 1,
      entries: [
        {
          aliases: [
            {
              created_at: "2026-08-01T09:00:00Z",
              email: "new-alias@icloud.com",
              label: "批量 1",
            },
          ],
          batch_id: "batch_123",
          complete: true,
          created: 1,
          created_at: "2026-08-01T09:00:00Z",
          error: "",
          failed: 0,
          label_prefix: "批量",
          requested: 1,
          status: "success",
          trigger: "batch",
        },
      ],
    };
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ data: history, success: true }))
      .mockResolvedValueOnce(textResponse("batch_id,email\nbatch_123,new-alias@icloud.com\n"));
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await expect(client.listAliasCreationHistory("account / id")).resolves.toEqual(history);
    await expect(client.downloadAliasCreationHistory("account / id")).resolves.toContain(
      "batch_123",
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      "/api/accounts/account%20%2F%20id/alias-creation-history?limit=100",
      expect.objectContaining({ method: "GET" }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      "/api/accounts/account%20%2F%20id/alias-creation-history.csv",
      expect.objectContaining({
        headers: expect.objectContaining({ Accept: "text/csv" }),
        method: "GET",
      }),
    );
  });

  it("uses account-scoped paths for batch creation and rule execution", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse({
          data: {
            account_id: "account / id",
            aliases: [
              {
                account_id: "account / id",
                created_at: "2026-08-01T09:00:00Z",
                email: "new-alias@icloud.com",
                label: "批量 1",
              },
            ],
            complete: true,
            created: 1,
            failed: 0,
            requested: 1,
          },
          success: true,
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          data: {
            account_id: "account / id",
            active_before: 4,
            aliases: [],
            automation: aliasAutomationFixture,
            complete: true,
            created: 0,
            failed: 0,
            requested: 0,
            status: "skipped",
            trigger: "manual",
          },
          success: true,
        }),
      );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await client.createAliasesBatch("account / id", { count: 1, labelPrefix: "批量" });
    await client.runAliasAutomation("account / id");

    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      "/api/accounts/account%20%2F%20id/aliases/batch",
      expect.objectContaining({
        body: JSON.stringify({ count: 1, label_prefix: "批量" }),
        method: "POST",
      }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      "/api/accounts/account%20%2F%20id/alias-automation/run",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("uses account-scoped pause and resume endpoints for automation rules", async () => {
    const fetcher = vi.fn(() =>
      Promise.resolve(
        jsonResponse({
          data: aliasAutomationFixture,
          success: true,
        }),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await client.pauseAliasAutomation("account / id");
    await client.resumeAliasAutomation("account / id");

    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      "/api/accounts/account%20%2F%20id/alias-automation/pause",
      expect.objectContaining({ method: "POST" }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      "/api/accounts/account%20%2F%20id/alias-automation/resume",
      expect.objectContaining({ method: "POST" }),
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

  it("loads a lightweight inbox before requesting one encoded message preview", async () => {
    const message = {
      date: "2026-08-02T04:34:00+08:00",
      from: "sender@example.com",
      id: "7/8",
      preview: "selected message body",
      subject: "Message subject",
      to: "alias@icloud.com",
    };
    const fetcher = vi.fn((url: string) =>
      Promise.resolve(
        jsonResponse({
          data: url.includes("/messages/")
            ? message
            : {
                account_id: "acc_main",
                alias: "",
                count: 1,
                messages: [{ ...message, id: "7", preview: "" }],
                method: "imap",
              },
          success: true,
        }),
      ),
    );
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await client.listInbox({ accountId: "acc_main", days: 7, limit: 20 });
    await client.getInboxMessage("account / id", "7/8");

    expect(fetcher.mock.calls[0]?.[0]).toBe(
      "/api/inbox?account_id=acc_main&days=7&include_preview=false&limit=20",
    );
    expect(fetcher.mock.calls[1]?.[0]).toBe("/api/inbox/messages/7%2F8?account_id=account+%2F+id");
  });

  it("sends the inbox cursor only when loading an older page", async () => {
    const fetcher = vi.fn((url: string) => {
      void url;
      return Promise.resolve(
        jsonResponse({
          data: {
            account_id: "acc_main",
            alias: "",
            count: 0,
            has_more: false,
            messages: [],
            method: "imap",
            next_cursor: "",
          },
          success: true,
        }),
      );
    });
    const client = createApiClient({ fetch: fetcher as unknown as typeof fetch });

    await client.listInbox({ accountId: "acc_main", beforeUid: "1040" });

    expect(fetcher.mock.calls[0]?.[0]).toBe(
      "/api/inbox?account_id=acc_main&before_uid=1040&include_preview=false",
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
