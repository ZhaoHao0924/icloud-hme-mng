import { HttpResponse, http } from "msw";

import {
  accountFixtures,
  aliasFixtures,
  degradedServiceFixture,
  healthyServiceFixture,
  inboxLongMessageFixtures,
  inboxMessageFixtures,
  mixedAccountFixtures,
  otpChallengeFixture,
} from "../fixtures";
import type { MockScenario } from "./scenario";
import {
  createMockAccountStore,
  type MockAccountStore,
  type MockCreateAccountInput,
} from "./accountStore";
import { createMockAliasStore, type MockAliasStore } from "./aliasStore";

type ScenarioReader = () => MockScenario;

const validMockOtpCode = "123456";

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
) {
  return [
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
      return successResponse(aliasStore.create({ accountId, label }));
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
      const messages = scenario === "inbox-long" ? inboxLongMessageFixtures : inboxMessageFixtures;
      return scenarioResponse(
        getScenario,
        {
          account_id: accountId,
          alias,
          count: messages.length,
          messages,
          method,
        },
        {
          account_id: accountId,
          alias,
          count: 0,
          messages: [],
          method,
        },
      );
    }),
    http.get("*/api/health", () =>
      scenarioResponse(getScenario, healthyServiceFixture, degradedServiceFixture),
    ),
    http.post("*/api/reload", () => {
      const failure = failureResponse(getScenario);
      if (failure) return failure;
      return successResponse({ message: "配置已重新加载" });
    }),
  ];
}
