import { HttpResponse, http } from "msw";

import type { AliasAutomation } from "../../api/schemas";
import {
  accountFixtures,
  aliasFixtures,
  degradedServiceFixture,
  healthyServiceFixture,
  inboxLongMessageFixtures,
  inboxMessageFixtures,
  mixedAccountFixtures,
  otpChallengeFixture,
  operationLogsFixture,
} from "../fixtures";
import type { MockScenario } from "./scenario";
import {
  createMockAccountStore,
  type MockAccountStore,
  type MockCreateAccountInput,
} from "./accountStore";
import { createMockAliasStore, type MockAliasStore } from "./aliasStore";
import {
  createMockAliasAutomationStore,
  type MockAliasAutomationInput,
  type MockAliasAutomationStore,
} from "./aliasAutomationStore";
import {
  createMockAliasCreationHistoryStore,
  type MockAliasCreationHistoryStore,
} from "./aliasCreationHistoryStore";
import { createMockPlatformAuthStore, type MockPlatformAuthStore } from "./platformAuthStore";
import {
  createMockEmailNotificationStore,
  type MockEmailNotificationStore,
} from "./emailNotificationStore";
import {
  createMockWebhookNotificationStore,
  type MockWebhookNotificationStore,
} from "./webhookNotificationStore";

type ScenarioReader = () => MockScenario;

const validMockOtpCode = "123456";

const inboxScrollMessageFixtures = Array.from({ length: 12 }, (_, index) => ({
  ...inboxMessageFixtures[index % inboxMessageFixtures.length],
  id: `scroll-${index + 1}`,
  subject: `滚动列表邮件 ${index + 1}`,
}));

function fixturesForScenario(scenario: MockScenario) {
  if (scenario === "empty") return [];
  return scenario === "mixed" ? mixedAccountFixtures : accountFixtures;
}

function aliasFixturesForScenario(scenario: MockScenario) {
  return scenario === "empty" ? [] : aliasFixtures;
}

function errorResponse() {
  return HttpResponse.json(
    {
      message: "模拟 Apple 服务错误",
      success: false,
    },
    { status: 502 },
  );
}

function inboxTimeoutResponse() {
  return HttpResponse.json(
    {
      message: "读取邮件超时，请稍后重试。",
      success: false,
    },
    { status: 504 },
  );
}

function offlineResponse() {
  return HttpResponse.error();
}

function sessionExpiredResponse(status = 401) {
  return HttpResponse.json(
    {
      message: "iCloud 会话失效，请更新 Cookie",
      success: false,
    },
    { status },
  );
}

function failureResponse(getScenario: ScenarioReader) {
  const scenario = getScenario();
  if (scenario === "offline") return offlineResponse();
  if (scenario === "error") return errorResponse();
  return null;
}

function aliasSessionFailureResponse(
  getScenario: ScenarioReader,
  accountStore: MockAccountStore,
  accountId: string,
) {
  const scenario = getScenario();
  if (
    (scenario === "expired" || scenario === "alias-forbidden") &&
    !accountStore.hasRestoredSession(accountId)
  ) {
    return sessionExpiredResponse(scenario === "alias-forbidden" ? 403 : 401);
  }
  return null;
}

function aliasAppleServiceFailureResponse(getScenario: ScenarioReader) {
  if (getScenario() === "alias-error") {
    return errorResponse();
  }
  return null;
}

function inboxFailureResponse(getScenario: ScenarioReader) {
  const scenario = getScenario();
  if (scenario === "inbox-error") return errorResponse();
  if (scenario === "inbox-timeout") return inboxTimeoutResponse();
  return null;
}

function mockClockMinutes(value: string) {
  if (!/^\d{2}:\d{2}$/.test(value)) return null;
  const [hours, minutes] = value.split(":").map(Number);
  if (hours > 23 || minutes > 59) return null;
  return hours * 60 + minutes;
}

function mockScheduleStatus(automation: AliasAutomation) {
  const now = new Date("2026-08-02T08:00:00.000Z");
  const weekdays = automation.allowed_weekdays;
  const weekdayAllowed = weekdays.length === 0 || weekdays.includes(now.getUTCDay());
  const start = mockClockMinutes(automation.execution_window_start);
  const end = mockClockMinutes(automation.execution_window_end);
  const hasWindow = start !== null && end !== null;
  const currentMinutes = now.getUTCHours() * 60 + now.getUTCMinutes();
  const scheduleAllowed =
    weekdayAllowed && (!hasWindow || (currentMinutes >= start && currentMinutes < end));
  if (scheduleAllowed) {
    return { nextEligibleAt: "", scheduleAllowed, scheduleReason: "" };
  }

  for (let offset = 0; offset <= 7; offset += 1) {
    const candidate = new Date(now);
    candidate.setUTCDate(now.getUTCDate() + offset);
    candidate.setUTCHours(0, 0, 0, 0);
    if (weekdays.length > 0 && !weekdays.includes(candidate.getUTCDay())) continue;
    if (!hasWindow) {
      return {
        nextEligibleAt: candidate.toISOString(),
        scheduleAllowed,
        scheduleReason: "当前不在允许的执行日",
      };
    }
    candidate.setUTCHours(Math.floor(start / 60), start % 60, 0, 0);
    if (offset === 0 && currentMinutes >= end) continue;
    return {
      nextEligibleAt: candidate.toISOString(),
      scheduleAllowed,
      scheduleReason: weekdayAllowed ? "当前不在执行时间窗内" : "当前不在允许的执行日",
    };
  }
  return { nextEligibleAt: "", scheduleAllowed, scheduleReason: "当前不在允许的执行日" };
}

function mockAutomationRequested(automation: AliasAutomation, activeAliases: number) {
  const replenish =
    automation.minimum_active > 0 && activeAliases < automation.minimum_active
      ? automation.target_active - activeAliases
      : 0;
  let requested = Math.min(
    automation.max_batch_size,
    Math.max(automation.scheduled_batch_size, replenish),
  );
  if (requested === 0 && automation.target_created > automation.created_total) {
    requested = automation.max_batch_size;
  }
  if (automation.target_created > 0) {
    requested = Math.min(
      requested,
      Math.max(0, automation.target_created - automation.created_total),
    );
  }
  return requested;
}

function mockAutomationPreview(
  accountId: string,
  automation: AliasAutomation,
  aliases: { active: boolean }[],
) {
  const activeAliases = aliases.filter((alias) => alias.active).length;
  const remainingTotalCapacity = Math.max(0, automation.max_total_aliases - aliases.length);
  const dailyRemaining =
    automation.daily_creation_limit > 0
      ? Math.max(0, automation.daily_creation_limit - automation.daily_created)
      : 0;
  const requested = Math.min(
    mockAutomationRequested(automation, activeAliases),
    remainingTotalCapacity,
    automation.daily_creation_limit > 0 ? dailyRemaining : Number.POSITIVE_INFINITY,
  );
  const schedule = mockScheduleStatus(automation);
  return {
    account_id: accountId,
    active_aliases: activeAliases,
    automation,
    daily_remaining: dailyRemaining,
    max_total_aliases: automation.max_total_aliases,
    next_eligible_at: schedule.nextEligibleAt,
    remaining_total_capacity: remainingTotalCapacity,
    requested,
    schedule_allowed: schedule.scheduleAllowed,
    schedule_reason: schedule.scheduleReason,
    target_remaining:
      automation.target_created > 0
        ? Math.max(0, automation.target_created - automation.created_total)
        : 0,
    total_aliases: aliases.length,
  };
}

function successResponse(data: unknown, status = 200) {
  return HttpResponse.json({ data, success: true }, { status });
}

function scenarioResponse(getScenario: ScenarioReader, successData: unknown, emptyData: unknown) {
  const scenario = getScenario();
  const failure = failureResponse(getScenario);
  if (failure) return failure;
  return successResponse(scenario === "empty" ? emptyData : successData);
}

export function createMockHandlers(
  getScenario: ScenarioReader,
  accountStore: MockAccountStore = createMockAccountStore(),
  aliasStore: MockAliasStore = createMockAliasStore(),
  automationStore: MockAliasAutomationStore = createMockAliasAutomationStore(),
  platformAuthStore: MockPlatformAuthStore = createMockPlatformAuthStore(),
  creationHistoryStore: MockAliasCreationHistoryStore = createMockAliasCreationHistoryStore(),
  emailNotificationStore: MockEmailNotificationStore = createMockEmailNotificationStore(),
  webhookNotificationStore: MockWebhookNotificationStore = createMockWebhookNotificationStore(),
) {
  return [
    http.get("*/api/auth/session", () => successResponse(platformAuthStore.status(getScenario()))),
    http.post("*/api/auth/login", async ({ request }) => {
      const status = platformAuthStore.login(
        (await request.json()) as Record<string, unknown>,
        getScenario(),
      );
      if (!status) {
        return HttpResponse.json(
          { code: "platform_auth_invalid", message: "用户名或密码错误", success: false },
          { status: 401 },
        );
      }
      return successResponse(status);
    }),
    http.post("*/api/auth/setup", async ({ request }) => {
      const status = platformAuthStore.setup(
        (await request.json()) as Record<string, unknown>,
        getScenario(),
      );
      if (!status) {
        return HttpResponse.json(
          { code: "platform_auth_invalid", message: "管理员账户初始化失败", success: false },
          { status: 400 },
        );
      }
      return successResponse(status);
    }),
    http.post("*/api/auth/logout", () => successResponse(platformAuthStore.logout())),
    http.get("*/api/notifications/email", () => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      return successResponse(emailNotificationStore.get());
    }),
    http.put("*/api/notifications/email", async ({ request }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const body = (await request.json()) as {
        authorization_code?: unknown;
        enabled?: unknown;
        sender_email?: unknown;
        recipient_email?: unknown;
      };
      if (
        typeof body.authorization_code !== "string" ||
        typeof body.enabled !== "boolean" ||
        typeof body.sender_email !== "string" ||
        typeof body.recipient_email !== "string"
      ) {
        return HttpResponse.json(
          { message: "QQ 閭閫氱煡鍙傛暟鏃犳晥", success: false },
          { status: 400 },
        );
      }
      return successResponse(
        emailNotificationStore.update({
          authorizationCode: body.authorization_code,
          enabled: body.enabled,
          senderEmail: body.sender_email,
          recipientEmail: body.recipient_email,
        }),
      );
    }),
    http.post("*/api/notifications/email/test", () => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      if (!emailNotificationStore.get().configured) {
        return HttpResponse.json(
          { message: "QQ 閭閫氱煡灏氭湭閰嶇疆", success: false },
          { status: 400 },
        );
      }
      return successResponse({ message: "163 test email sent" });
    }),
    http.get("*/api/notifications/webhook", () => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      return successResponse(webhookNotificationStore.get());
    }),
    http.put("*/api/notifications/webhook", async ({ request }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const body = (await request.json()) as {
        enabled?: unknown;
        secret?: unknown;
        url?: unknown;
      };
      if (
        typeof body.enabled !== "boolean" ||
        typeof body.secret !== "string" ||
        typeof body.url !== "string"
      ) {
        return HttpResponse.json(
          { message: "webhook notification parameters are invalid", success: false },
          { status: 400 },
        );
      }
      return successResponse(
        webhookNotificationStore.update({
          enabled: body.enabled,
          secret: body.secret,
          url: body.url,
        }),
      );
    }),
    http.post("*/api/notifications/webhook/test", () => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      if (!webhookNotificationStore.get().configured) {
        return HttpResponse.json(
          { message: "webhook notification is not configured", success: false },
          { status: 400 },
        );
      }
      return successResponse({ message: "webhook test delivery completed" });
    }),
    http.get("*/api/accounts", () => {
      const scenario = getScenario();
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      return successResponse(accountStore.list(fixturesForScenario(scenario)));
    }),
    http.post("*/api/accounts", async ({ request }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const input = (await request.json()) as MockCreateAccountInput;
      if (!input.name?.trim()) {
        return HttpResponse.json(
          { message: "参数错误: name 必填", success: false },
          { status: 400 },
        );
      }
      return successResponse(accountStore.create(input), 201);
    }),
    http.delete("*/api/accounts/:accountId", ({ params }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      return successResponse(accountStore.delete(String(params.accountId)));
    }),
    http.put("*/api/accounts/:accountId/cookies", async ({ params, request }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const accountId = String(params.accountId);
      const body = (await request.json()) as { cookies?: unknown };
      const cookies = body.cookies;
      if (
        typeof cookies !== "object" ||
        cookies === null ||
        Array.isArray(cookies) ||
        Object.keys(cookies).length === 0 ||
        Object.keys(cookies).length > 128 ||
        Object.values(cookies).some((value) => typeof value !== "string")
      ) {
        return HttpResponse.json(
          { message: "参数错误: cookies 格式无效", success: false },
          { status: 400 },
        );
      }
      const account = accountStore.get(accountId, fixturesForScenario(getScenario()));
      if (!account) {
        return HttpResponse.json({ message: "账号不存在", success: false }, { status: 404 });
      }
      return successResponse(
        (() => {
          accountStore.markSessionRestored(accountId);
          return accountStore.update({
            ...account,
            has_cookies: true,
            last_error: "",
            last_validated: "2026-08-01T11:00:00+08:00",
            status: "active",
          });
        })(),
      );
    }),
    http.post("*/api/accounts/:accountId/password", async ({ params, request }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const accountId = String(params.accountId);
      const body = (await request.json()) as {
        app_password?: unknown;
        icloud_email?: unknown;
      };
      if (
        typeof body.icloud_email !== "string" ||
        body.icloud_email.trim() === "" ||
        typeof body.app_password !== "string" ||
        body.app_password.trim() === ""
      ) {
        return HttpResponse.json(
          { message: "参数错误: icloud_email, app_password 必填", success: false },
          { status: 400 },
        );
      }
      const account = accountStore.get(accountId, fixturesForScenario(getScenario()));
      if (!account) {
        return HttpResponse.json({ message: "账号不存在", success: false }, { status: 404 });
      }
      return successResponse(
        accountStore.update({
          ...account,
          has_app_password: true,
          icloud_email: body.icloud_email.trim(),
        }),
      );
    }),
    http.post("*/api/accounts/:accountId/login/start", async ({ params, request }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const accountId = String(params.accountId);
      const body = (await request.json()) as { password?: unknown };
      if (typeof body.password !== "string" || body.password.trim() === "") {
        return HttpResponse.json(
          { message: "参数错误: password 必填", success: false },
          { status: 400 },
        );
      }
      accountStore.invalidateLoginChallenge(accountId);
      const account = accountStore.get(accountId, fixturesForScenario(getScenario()));
      if (!account) {
        return HttpResponse.json({ message: "账号不存在", success: false }, { status: 404 });
      }
      if (getScenario() === "otp") {
        accountStore.issueLoginChallenge(accountId, otpChallengeFixture.challenge_id);
        return successResponse(otpChallengeFixture);
      }
      return successResponse(
        (() => {
          accountStore.markSessionRestored(accountId);
          return accountStore.update({
            ...account,
            has_cookies: true,
            last_error: "",
            last_validated: "2026-08-01T12:00:00+08:00",
            status: "active",
          });
        })(),
      );
    }),
    http.post("*/api/accounts/:accountId/login/verify", async ({ params, request }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const accountId = String(params.accountId);
      const body = (await request.json()) as {
        challenge_id?: unknown;
        otp_code?: unknown;
      };
      const challengeId = typeof body.challenge_id === "string" ? body.challenge_id.trim() : "";
      const otpCode = typeof body.otp_code === "string" ? body.otp_code.trim() : "";
      if (challengeId === "" || challengeId.length > 128) {
        return HttpResponse.json(
          { message: "参数错误: challenge_id 格式无效", success: false },
          { status: 400 },
        );
      }
      if (!/^\d{6}$/.test(otpCode)) {
        return HttpResponse.json(
          { message: "参数错误: otp_code 必须是 6 位数字", success: false },
          { status: 400 },
        );
      }
      if (!accountStore.consumeLoginChallenge(accountId, challengeId)) {
        return HttpResponse.json(
          { message: "登录 challenge 无效或已过期，请重新提交密码", success: false },
          { status: 410 },
        );
      }
      const account = accountStore.get(accountId, fixturesForScenario(getScenario()));
      if (!account) {
        return HttpResponse.json({ message: "账号不存在", success: false }, { status: 404 });
      }
      if (otpCode !== validMockOtpCode) {
        return HttpResponse.json(
          { message: "验证码验证失败: 双重认证验证码无效", success: false },
          { status: 401 },
        );
      }
      return successResponse(
        (() => {
          accountStore.markSessionRestored(accountId);
          return accountStore.update({
            ...account,
            has_cookies: true,
            last_error: "",
            last_validated: "2026-08-01T12:05:00+08:00",
            status: "active",
          });
        })(),
      );
    }),
    http.get("*/api/accounts/:accountId/alias-automation", ({ params }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const accountId = String(params.accountId);
      if (!accountStore.get(accountId, fixturesForScenario(getScenario()))) {
        return HttpResponse.json({ message: "账号不存在", success: false }, { status: 404 });
      }
      return successResponse(automationStore.get(accountId));
    }),
    http.get("*/api/accounts/:accountId/alias-creation-history", ({ params }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const accountId = String(params.accountId);
      if (!accountStore.get(accountId, fixturesForScenario(getScenario()))) {
        return HttpResponse.json({ message: "账号不存在", success: false }, { status: 404 });
      }
      return successResponse(creationHistoryStore.get(accountId));
    }),
    http.get("*/api/accounts/:accountId/alias-creation-history.csv", ({ params }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const accountId = String(params.accountId);
      if (!accountStore.get(accountId, fixturesForScenario(getScenario()))) {
        return HttpResponse.json({ message: "账号不存在", success: false }, { status: 404 });
      }
      const history = creationHistoryStore.get(accountId);
      const rows = history.entries.flatMap((entry) => {
        const aliases =
          entry.aliases.length > 0 ? entry.aliases : [{ created_at: "", email: "", label: "" }];
        return aliases.map((alias) =>
          [
            entry.batch_id,
            entry.created_at,
            entry.trigger,
            entry.status,
            entry.requested,
            entry.created,
            entry.failed,
            entry.complete,
            entry.label_prefix,
            entry.error,
            alias.email,
            alias.label,
            alias.created_at,
          ].join(","),
        );
      });
      return HttpResponse.text(
        [
          "batch_id,created_at,trigger,status,requested,created,failed,complete,label_prefix,error,email,alias_label,alias_created_at",
          ...rows,
        ].join("\n"),
        { headers: { "Content-Type": "text/csv; charset=utf-8" } },
      );
    }),
    http.put("*/api/accounts/:accountId/alias-automation", async ({ params, request }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const accountId = String(params.accountId);
      if (!accountStore.get(accountId, fixturesForScenario(getScenario()))) {
        return HttpResponse.json({ message: "账号不存在", success: false }, { status: 404 });
      }
      const body = (await request.json()) as Partial<MockAliasAutomationInput>;
      const allowedWeekdays = body.allowed_weekdays ?? [];
      const executionWindowStart = body.execution_window_start ?? "";
      const executionWindowEnd = body.execution_window_end ?? "";
      if (
        typeof body.enabled !== "boolean" ||
        !Number.isInteger(body.interval_minutes) ||
        !Number.isInteger(body.scheduled_batch_size) ||
        !Number.isInteger(body.minimum_active) ||
        !Number.isInteger(body.target_active) ||
        !Number.isInteger(body.max_batch_size) ||
        !Number.isInteger(body.max_total_aliases) ||
        !Number.isInteger(body.max_failure_count) ||
        !Number.isInteger(body.daily_creation_limit) ||
        !Number.isInteger(body.target_created) ||
        !Array.isArray(allowedWeekdays) ||
        typeof executionWindowStart !== "string" ||
        typeof executionWindowEnd !== "string" ||
        typeof body.label_prefix !== "string"
      ) {
        return HttpResponse.json(
          { message: "参数错误: 自动化规则无效", success: false },
          { status: 400 },
        );
      }
      const input = {
        ...body,
        allowed_weekdays: allowedWeekdays,
        execution_window_start: executionWindowStart,
        execution_window_end: executionWindowEnd,
      } as Required<MockAliasAutomationInput>;

      const windowStart = mockClockMinutes(input.execution_window_start);
      const windowEnd = mockClockMinutes(input.execution_window_end);
      if (
        input.interval_minutes < 5 ||
        input.interval_minutes > 10080 ||
        input.max_batch_size < 1 ||
        input.max_batch_size > 20 ||
        input.max_total_aliases < 1 ||
        input.max_total_aliases > 1000 ||
        input.max_failure_count < 1 ||
        input.max_failure_count > 10 ||
        input.daily_creation_limit < 0 ||
        input.daily_creation_limit > 1000 ||
        input.scheduled_batch_size < 0 ||
        input.scheduled_batch_size > input.max_batch_size ||
        input.minimum_active < 0 ||
        input.minimum_active > 100 ||
        input.target_active < input.minimum_active ||
        input.target_active > 100 ||
        input.target_created < 0 ||
        input.target_created > 1000 ||
        input.allowed_weekdays.some(
          (weekday) => !Number.isInteger(weekday) || weekday < 0 || weekday > 6,
        ) ||
        new Set(input.allowed_weekdays).size !== input.allowed_weekdays.length ||
        (input.execution_window_start === "") !== (input.execution_window_end === "") ||
        (input.execution_window_start !== "" &&
          input.execution_window_end !== "" &&
          (windowStart === null || windowEnd === null || windowStart >= windowEnd)) ||
        (input.minimum_active === 0 && input.target_active !== 0) ||
        (input.enabled &&
          input.scheduled_batch_size === 0 &&
          input.minimum_active === 0 &&
          input.target_created === 0) ||
        Array.from(input.label_prefix.trim()).length > 196
      ) {
        return HttpResponse.json(
          { message: "参数错误: 自动化规则无效", success: false },
          { status: 400 },
        );
      }
      return successResponse(automationStore.update(accountId, input));
    }),
    http.post("*/api/accounts/:accountId/alias-automation/preview", ({ params }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const accountId = String(params.accountId);
      if (!accountStore.get(accountId, fixturesForScenario(getScenario()))) {
        return HttpResponse.json({ message: "账号不存在", success: false }, { status: 404 });
      }
      const aliasFailure = aliasAppleServiceFailureResponse(getScenario);
      if (aliasFailure) return aliasFailure;
      const sessionFailure = aliasSessionFailureResponse(getScenario, accountStore, accountId);
      if (sessionFailure) return sessionFailure;
      const automation = automationStore.get(accountId);
      if (
        automation.scheduled_batch_size === 0 &&
        automation.minimum_active === 0 &&
        automation.target_created === 0
      ) {
        return HttpResponse.json(
          { message: "请先配置定时创建数量、库存阈值或累计创建目标", success: false },
          { status: 400 },
        );
      }
      const aliases = aliasStore.list(accountId, aliasFixturesForScenario(getScenario()));
      return successResponse(mockAutomationPreview(accountId, automation, aliases));
    }),
    http.post("*/api/accounts/:accountId/alias-automation/pause", ({ params }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const accountId = String(params.accountId);
      if (!accountStore.get(accountId, fixturesForScenario(getScenario()))) {
        return HttpResponse.json({ message: "账号不存在", success: false }, { status: 404 });
      }
      return successResponse(automationStore.pause(accountId));
    }),
    http.post("*/api/accounts/:accountId/alias-automation/resume", ({ params }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const accountId = String(params.accountId);
      if (!accountStore.get(accountId, fixturesForScenario(getScenario()))) {
        return HttpResponse.json({ message: "账号不存在", success: false }, { status: 404 });
      }
      const automation = automationStore.resume(accountId);
      if (!automation) {
        return HttpResponse.json(
          { message: "累计创建目标已完成，请修改目标后再恢复", success: false },
          { status: 400 },
        );
      }
      return successResponse(automation);
    }),
    http.post("*/api/accounts/:accountId/aliases/batch", async ({ params, request }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const accountId = String(params.accountId);
      const body = (await request.json()) as { count?: unknown; label_prefix?: unknown };
      const count = body.count;
      const labelPrefix = typeof body.label_prefix === "string" ? body.label_prefix.trim() : "";
      if (!Number.isInteger(count) || typeof count !== "number" || count < 1 || count > 20) {
        return HttpResponse.json(
          { message: "参数错误: count 无效", success: false },
          { status: 400 },
        );
      }
      if (Array.from(labelPrefix).length > 196) {
        return HttpResponse.json(
          { message: "参数错误: label_prefix 无效", success: false },
          { status: 400 },
        );
      }
      const aliasFailure = aliasAppleServiceFailureResponse(getScenario);
      if (aliasFailure) return aliasFailure;
      const sessionFailure = aliasSessionFailureResponse(getScenario, accountStore, accountId);
      if (sessionFailure) return sessionFailure;
      const aliases = Array.from({ length: count }, (_, index) =>
        aliasStore.create({
          accountId,
          label: count === 1 || labelPrefix === "" ? labelPrefix : `${labelPrefix} ${index + 1}`,
        }),
      );
      const history = creationHistoryStore.record(accountId, {
        aliases: aliases.map((alias) => ({
          created_at: alias.created_at,
          email: alias.email,
          label: alias.label,
        })),
        complete: true,
        created: aliases.length,
        error: "",
        failed: 0,
        label_prefix: labelPrefix,
        requested: count,
        status: "success",
        trigger: "batch",
      });
      return successResponse({
        account_id: accountId,
        aliases: aliases.map((alias) => ({ ...alias, batch_id: history.batch_id })),
        batch_id: history.batch_id,
        complete: true,
        created: aliases.length,
        failed: 0,
        requested: count,
      });
    }),
    http.post("*/api/accounts/:accountId/alias-automation/run", ({ params }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const accountId = String(params.accountId);
      const aliasFailure = aliasAppleServiceFailureResponse(getScenario);
      if (aliasFailure) return aliasFailure;
      const sessionFailure = aliasSessionFailureResponse(getScenario, accountStore, accountId);
      if (sessionFailure) return sessionFailure;
      const automation = automationStore.get(accountId);
      if (
        automation.scheduled_batch_size === 0 &&
        automation.minimum_active === 0 &&
        automation.target_created === 0
      ) {
        return HttpResponse.json(
          { message: "请先配置定时创建数量、库存阈值或累计创建目标", success: false },
          { status: 400 },
        );
      }
      const activeBefore = aliasStore
        .list(accountId, aliasFixturesForScenario(getScenario()))
        .filter((alias) => alias.active).length;
      const replenish =
        automation.minimum_active > 0 && activeBefore < automation.minimum_active
          ? automation.target_active - activeBefore
          : 0;
      let requested = Math.min(
        automation.max_batch_size,
        Math.max(automation.scheduled_batch_size, replenish),
      );
      if (requested === 0 && automation.target_created > automation.created_total) {
        requested = automation.max_batch_size;
      }
      if (automation.target_created > 0) {
        requested = Math.min(
          requested,
          Math.max(0, automation.target_created - automation.created_total),
        );
      }
      let dailyLimitReached = false;
      if (requested > 0 && automation.daily_creation_limit > 0) {
        const dailyRemaining = Math.max(
          0,
          automation.daily_creation_limit - automation.daily_created,
        );
        if (dailyRemaining <= 0) {
          requested = 0;
          dailyLimitReached = true;
        } else if (requested >= dailyRemaining) {
          requested = dailyRemaining;
          dailyLimitReached = true;
        }
      }
      const aliases = Array.from({ length: requested }, (_, index) =>
        aliasStore.create({
          accountId,
          label:
            requested === 1 || automation.label_prefix === ""
              ? automation.label_prefix
              : `${automation.label_prefix} ${index + 1}`,
        }),
      );
      const status = requested === 0 ? "skipped" : "success";
      const dailyLimitMessage = dailyLimitReached
        ? `已达到今日自动创建上限 ${automation.daily_creation_limit}，将在次日继续`
        : "";
      const updatedAutomation = automationStore.recordRun(accountId, {
        active: activeBefore + aliases.length,
        created: aliases.length,
        error: dailyLimitMessage,
        nextRunAt: dailyLimitReached ? "2026-08-03T00:00:00.000Z" : undefined,
        status,
      });
      const history = creationHistoryStore.record(accountId, {
        aliases: aliases.map((alias) => ({
          created_at: alias.created_at,
          email: alias.email,
          label: alias.label,
        })),
        complete: true,
        created: aliases.length,
        error: dailyLimitMessage,
        failed: 0,
        label_prefix: automation.label_prefix,
        requested,
        status,
        trigger: "automation_manual",
      });
      return successResponse({
        account_id: accountId,
        active_before: activeBefore,
        aliases: aliases.map((alias) => ({ ...alias, batch_id: history.batch_id })),
        automation: updatedAutomation,
        batch_id: history.batch_id,
        complete: true,
        created: aliases.length,
        error: dailyLimitMessage,
        failed: 0,
        requested,
        status,
        trigger: "manual",
      });
    }),
    http.get("*/api/aliases", ({ request }) => {
      const accountId = new URL(request.url).searchParams.get("account_id") ?? "";
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const aliasFailure = aliasAppleServiceFailureResponse(getScenario);
      if (aliasFailure) return aliasFailure;
      const sessionFailure = aliasSessionFailureResponse(getScenario, accountStore, accountId);
      if (sessionFailure) return sessionFailure;
      const aliases = aliasStore.list(accountId, aliasFixturesForScenario(getScenario()));
      return successResponse({ account_id: accountId, aliases, count: aliases.length });
    }),
    http.post("*/api/create", async ({ request }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const aliasFailure = aliasAppleServiceFailureResponse(getScenario);
      if (aliasFailure) return aliasFailure;
      const body = (await request.json()) as { account_id?: unknown; label?: unknown };
      const accountId = typeof body.account_id === "string" ? body.account_id.trim() : "";
      const label = typeof body.label === "string" ? body.label : "";
      const sessionFailure = aliasSessionFailureResponse(getScenario, accountStore, accountId);
      if (sessionFailure) return sessionFailure;
      if (accountId === "") {
        return HttpResponse.json(
          { message: "参数错误: account_id 必填", success: false },
          { status: 400 },
        );
      }
      if (Array.from(label).length > 200) {
        return HttpResponse.json(
          { message: "参数错误: label 不能超过 200 个字符", success: false },
          { status: 400 },
        );
      }
      const alias = aliasStore.create({ accountId, label });
      const history = creationHistoryStore.record(accountId, {
        aliases: [
          {
            created_at: alias.created_at,
            email: alias.email,
            label: alias.label,
          },
        ],
        complete: true,
        created: 1,
        error: "",
        failed: 0,
        label_prefix: label,
        requested: 1,
        status: "success",
        trigger: "manual",
      });
      return successResponse({ ...alias, batch_id: history.batch_id });
    }),
    http.post("*/api/aliases/:aliasId/deactivate", async ({ params, request }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const aliasFailure = aliasAppleServiceFailureResponse(getScenario);
      if (aliasFailure) return aliasFailure;
      const body = (await request.json()) as { account_id?: unknown };
      const accountId = typeof body.account_id === "string" ? body.account_id.trim() : "";
      const aliasId = String(params.aliasId);
      const sessionFailure = aliasSessionFailureResponse(getScenario, accountStore, accountId);
      if (sessionFailure) return sessionFailure;
      if (
        accountId === "" ||
        !aliasStore.setActive(accountId, aliasId, false, aliasFixturesForScenario(getScenario()))
      ) {
        return HttpResponse.json({ message: "别名不存在", success: false }, { status: 404 });
      }
      return successResponse({ anonymous_id: aliasId, success: true });
    }),
    http.post("*/api/aliases/:aliasId/reactivate", async ({ params, request }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const aliasFailure = aliasAppleServiceFailureResponse(getScenario);
      if (aliasFailure) return aliasFailure;
      const body = (await request.json()) as { account_id?: unknown };
      const accountId = typeof body.account_id === "string" ? body.account_id.trim() : "";
      const aliasId = String(params.aliasId);
      const sessionFailure = aliasSessionFailureResponse(getScenario, accountStore, accountId);
      if (sessionFailure) return sessionFailure;
      if (
        accountId === "" ||
        !aliasStore.setActive(accountId, aliasId, true, aliasFixturesForScenario(getScenario()))
      ) {
        return HttpResponse.json({ message: "别名不存在", success: false }, { status: 404 });
      }
      return successResponse({ anonymous_id: aliasId, success: true });
    }),
    http.delete("*/api/aliases/:aliasId", async ({ params, request }) => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      const aliasFailure = aliasAppleServiceFailureResponse(getScenario);
      if (aliasFailure) return aliasFailure;
      const body = (await request.json()) as { account_id?: unknown };
      const accountId = typeof body.account_id === "string" ? body.account_id.trim() : "";
      const aliasId = String(params.aliasId);
      const sessionFailure = aliasSessionFailureResponse(getScenario, accountStore, accountId);
      if (sessionFailure) return sessionFailure;
      if (
        accountId === "" ||
        !aliasStore.delete(accountId, aliasId, aliasFixturesForScenario(getScenario()))
      ) {
        return HttpResponse.json({ message: "别名不存在", success: false }, { status: 404 });
      }
      return successResponse({ anonymous_id: aliasId });
    }),
    http.get("*/api/inbox", ({ request }) => {
      const failure = inboxFailureResponse(getScenario);
      if (failure) return failure;
      const url = new URL(request.url);
      const accountId = url.searchParams.get("account_id") ?? "";
      const alias = url.searchParams.get("alias") ?? "";
      const scenario = getScenario();
      const method = scenario === "web-api" ? "web_api" : "imap";
      if (scenario === "inbox-paged") {
        const nextPage = url.searchParams.get("before_uid") === "1040";
        const messages = nextPage
          ? inboxMessageFixtures.slice(2)
          : inboxMessageFixtures.slice(0, 2);
        return successResponse({
          account_id: accountId,
          alias,
          count: messages.length,
          has_more: !nextPage,
          messages,
          method,
          next_cursor: nextPage ? "" : "1040",
        });
      }
      const messages =
        scenario === "inbox-html"
          ? inboxMessageFixtures.map((message) => ({ ...message, preview: "" }))
          : scenario === "inbox-long"
            ? inboxLongMessageFixtures
            : scenario === "inbox-scroll"
              ? inboxScrollMessageFixtures
              : inboxMessageFixtures;
      return scenarioResponse(
        getScenario,
        {
          account_id: accountId,
          alias,
          count: messages.length,
          has_more: false,
          messages,
          method,
          next_cursor: "",
        },
        {
          account_id: accountId,
          alias,
          count: 0,
          has_more: false,
          messages: [],
          method,
          next_cursor: "",
        },
      );
    }),
    http.get("*/api/inbox/messages/:messageId", ({ params }) => {
      const messageId = String(params.messageId);
      const message = inboxMessageFixtures.find((candidate) => candidate.id === messageId);
      if (!message) {
        return HttpResponse.json({ message: "邮件不存在", success: false }, { status: 404 });
      }
      if (getScenario() === "inbox-html") {
        return successResponse({
          ...message,
          body: `<!doctype html><html><head><style>.action { display: inline-block; padding: 10px 14px; color: white; background: #1463d2; }</style></head><body><table style="width: 960px"><tr><td><p>请继续完成操作。</p><a class="action" href="https://example.test/continue">打开链接</a><p>unbroken-email-content-${"x".repeat(180)}</p></td></tr></table><script>window.top.location = "https://attacker.test"</script></body></html>`,
          content_type: "text/html",
          preview: "请继续完成操作。",
        });
      }
      return successResponse({
        ...message,
        body: message.preview,
        content_type: "text/plain",
      });
    }),
    http.get("*/api/health", () =>
      scenarioResponse(getScenario, healthyServiceFixture, degradedServiceFixture),
    ),
    http.get("*/api/logs", () =>
      scenarioResponse(getScenario, operationLogsFixture, {
        count: 0,
        entries: [],
        retention_days: 7,
      }),
    ),
    http.post("*/api/reload", () => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      return successResponse({ message: "配置已重新加载" });
    }),
  ];
}
