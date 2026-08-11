import { z } from "zod";

import { getApiToken } from "./apiTokenSession";
import {
  accountSchema,
  aliasActionSchema,
  aliasCreationHistorySchema,
  aliasAutomationPreviewSchema,
  aliasAutomationRunSchema,
  aliasAutomationSchema,
  aliasBatchResultSchema,
  aliasesSchema,
  apiEnvelopeSchema,
  createdAliasSchema,
  deletedAccountSchema,
  deletedAliasSchema,
  emailNotificationSchema,
  emailNotificationTestSchema,
  healthSchema,
  inboxMessageSchema,
  inboxSchema,
  operationLogsSchema,
  otpChallengeSchema,
  platformAuthStatusSchema,
  reloadedConfigSchema,
  webhookNotificationSchema,
  webhookNotificationTestSchema,
  type Account,
  type AliasAction,
  type AliasCreationHistory,
  type AliasAutomation,
  type AliasAutomationPreview,
  type AliasAutomationRun,
  type AliasBatchResult,
  type Aliases,
  type CreatedAlias,
  type DeletedAccount,
  type DeletedAlias,
  type EmailNotification,
  type EmailNotificationTestResult,
  type Health,
  type Inbox,
  type InboxMessage,
  type OtpChallenge,
  type PlatformAuthStatus,
  type ReloadedConfig,
  type WebhookNotification,
  type WebhookNotificationTestResult,
} from "./schemas";

const defaultApiBaseUrl = "/api";
const aliasCreationHistoryRequestLimit = 500;

export type ApiErrorKind = "http" | "network" | "invalid_response" | "timeout";

export class ApiError extends Error {
  readonly code: string | null;
  readonly kind: ApiErrorKind;
  readonly retryable: boolean;
  readonly status: number | null;

  constructor({
    code,
    kind,
    message,
    status,
  }: {
    code?: string;
    kind: ApiErrorKind;
    message: string;
    status?: number;
  }) {
    super(message);
    this.name = "ApiError";
    this.code = code ?? null;
    this.kind = kind;
    this.status = status ?? null;
    this.retryable = kind === "network";
  }
}

export type ApiRequestOptions = {
  signal?: AbortSignal;
  timeoutMs?: number;
};

export type CreateAccountInput = {
  name: string;
  icloudEmail?: string;
  cookies?: string;
  host?: string;
  proxy?: string;
};

export type CreateAliasInput = {
  accountId: string;
  label?: string;
};

export type CreateAliasesBatchInput = {
  count: number;
  labelPrefix?: string;
};

export type AliasAutomationInput = {
  enabled: boolean;
  intervalMinutes: number;
  allowedWeekdays?: number[];
  executionWindowStart?: string;
  executionWindowEnd?: string;
  scheduledBatchSize: number;
  minimumActive: number;
  targetActive: number;
  maxBatchSize: number;
  maxTotalAliases: number;
  maxFailureCount: number;
  dailyCreationLimit: number;
  targetCreated: number;
  labelPrefix: string;
};

export type InboxQuery = {
  accountId: string;
  alias?: string;
  beforeUid?: string;
  limit?: number;
  days?: number;
};

export type SetAppPasswordInput = {
  icloudEmail: string;
  appPassword: string;
};

export type PlatformAuthCredentials = {
  password: string;
  username: string;
};

export type EmailNotificationInput = {
  enabled: boolean;
  senderEmail: string;
  authorizationCode: string;
  recipientEmail: string;
};

export type WebhookNotificationInput = {
  enabled: boolean;
  url: string;
  secret: string;
};

export type StartLoginResult = Account | OtpChallenge;

export type ApiClientOptions = {
  apiTokenProvider?: () => string | undefined;
  baseUrl?: string;
  fetch?: typeof fetch;
};

type QueryValue = boolean | number | string | undefined;

type RequestConfig<TSchema extends z.ZodType> = {
  body?: unknown;
  method: "DELETE" | "GET" | "POST" | "PUT";
  path: string;
  query?: Record<string, QueryValue>;
  responseSchema: TSchema;
  signal?: AbortSignal;
  timeoutMs?: number;
};

function isAbortError(error: unknown) {
  return (
    typeof error === "object" &&
    error !== null &&
    "name" in error &&
    (error as { name?: unknown }).name === "AbortError"
  );
}

function createRequestAbortContext(signal?: AbortSignal, timeoutMs?: number) {
  if (timeoutMs === undefined) {
    return {
      cleanup: () => undefined,
      didTimeout: () => false,
      signal,
    };
  }

  const controller = new AbortController();
  let timedOut = false;
  const abortForParentSignal = () => controller.abort();
  if (signal?.aborted) {
    abortForParentSignal();
  } else {
    signal?.addEventListener("abort", abortForParentSignal, { once: true });
  }
  const timeout = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, timeoutMs);

  return {
    cleanup: () => {
      clearTimeout(timeout);
      signal?.removeEventListener("abort", abortForParentSignal);
    },
    didTimeout: () => timedOut,
    signal: controller.signal,
  };
}

function createUrl(baseUrl: string, path: string, query?: Record<string, QueryValue>) {
  const queryString = new URLSearchParams();
  for (const [name, value] of Object.entries(query ?? {})) {
    if (value !== undefined) {
      queryString.set(name, String(value));
    }
  }

  const normalizedBaseUrl = baseUrl.replace(/\/+$/, "");
  const normalizedPath = path.replace(/^\/+/, "");
  const suffix = queryString.size > 0 ? `?${queryString.toString()}` : "";
  return `${normalizedBaseUrl}/${normalizedPath}${suffix}`;
}

function endpointPath(...parts: string[]) {
  return parts.map((part) => encodeURIComponent(part)).join("/");
}

function fallbackHttpMessage(status: number) {
  return `请求失败（HTTP ${status}）`;
}

async function responsePayload(response: Response): Promise<unknown> {
  try {
    return await response.json();
  } catch (error) {
    if (isAbortError(error)) {
      throw error;
    }
    throw new ApiError({
      kind: "invalid_response",
      message: "服务响应格式无效，请检查服务版本。",
      status: response.status,
    });
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

export function isApiTokenError(error: unknown) {
  return isApiError(error) && error.code === "api_token_invalid";
}

export function isPlatformAuthError(error: unknown) {
  return (
    isApiError(error) &&
    (error.code === "platform_auth_required" || error.code === "platform_auth_setup_required")
  );
}

export function isSessionExpiredError(error: unknown) {
  return (
    isApiError(error) &&
    !isApiTokenError(error) &&
    !isPlatformAuthError(error) &&
    (error.status === 401 || error.status === 403)
  );
}

export function getApiErrorMessage(error: unknown) {
  if (!isApiError(error)) {
    return "请求失败，请稍后重试。";
  }
  if (isApiTokenError(error)) {
    return "需要 API 访问令牌，请输入后继续。";
  }
  if (isPlatformAuthError(error)) {
    return "登录会话已失效，请重新登录。";
  }
  if (isSessionExpiredError(error)) {
    return "会话已过期，请更新 Cookie。";
  }
  if (error.status === 502) {
    return `Apple 服务错误：${error.message}`;
  }
  return error.message;
}

export function shouldRetryApiRequest(failureCount: number, error: unknown) {
  if (failureCount >= 1 || isAbortError(error)) {
    return false;
  }
  return !isApiError(error) || error.retryable;
}

export function createApiClient(options: ApiClientOptions = {}) {
  const apiTokenProvider = options.apiTokenProvider ?? getApiToken;
  const baseUrl = options.baseUrl ?? defaultApiBaseUrl;
  const configuredFetch = options.fetch;

  async function request<TSchema extends z.ZodType>({
    body,
    method,
    path,
    query,
    responseSchema,
    signal,
    timeoutMs,
  }: RequestConfig<TSchema>): Promise<z.output<TSchema>> {
    const abortContext = createRequestAbortContext(signal, timeoutMs);
    try {
      const fetcher = configuredFetch ?? globalThis.fetch;
      const requestUrl = createUrl(baseUrl, path, query);
      const resolvedUrl =
        configuredFetch || typeof window === "undefined"
          ? requestUrl
          : new URL(requestUrl, window.location.origin).toString();
      const apiToken = apiTokenProvider()?.trim();
      const response = await fetcher(resolvedUrl, {
        body: body === undefined ? undefined : JSON.stringify(body),
        cache: "no-store",
        credentials: "same-origin",
        headers: {
          Accept: "application/json",
          ...(apiToken ? { Authorization: `Bearer ${apiToken}` } : {}),
          ...(body === undefined ? {} : { "Content-Type": "application/json" }),
        },
        method,
        signal: abortContext.signal,
      });

      const payload = await responsePayload(response);
      const envelope = apiEnvelopeSchema.safeParse(payload);
      if (!envelope.success) {
        throw new ApiError({
          kind: "invalid_response",
          message: "服务响应格式无效，请检查服务版本。",
          status: response.status,
        });
      }

      if (!response.ok || !envelope.data.success) {
        throw new ApiError({
          code: envelope.data.code,
          kind: "http",
          message: envelope.data.message?.trim() || fallbackHttpMessage(response.status),
          status: response.status,
        });
      }

      const parsed = responseSchema.safeParse(envelope.data.data);
      if (!parsed.success) {
        throw new ApiError({
          kind: "invalid_response",
          message: "服务响应格式无效，请检查服务版本。",
          status: response.status,
        });
      }
      return parsed.data;
    } catch (error) {
      if (isAbortError(error)) {
        if (abortContext.didTimeout()) {
          throw new ApiError({
            kind: "timeout",
            message: "读取邮件超时，请稍后重试。",
          });
        }
        throw error;
      }
      if (isApiError(error)) {
        throw error;
      }
      throw new ApiError({
        kind: "network",
        message: "无法连接到本地服务，请确认服务已启动。",
      });
    } finally {
      abortContext.cleanup();
    }
  }

  async function requestText(path: string, signal?: AbortSignal): Promise<string> {
    const abortContext = createRequestAbortContext(signal);
    try {
      const fetcher = configuredFetch ?? globalThis.fetch;
      const requestUrl = createUrl(baseUrl, path);
      const resolvedUrl =
        configuredFetch || typeof window === "undefined"
          ? requestUrl
          : new URL(requestUrl, window.location.origin).toString();
      const apiToken = apiTokenProvider()?.trim();
      const response = await fetcher(resolvedUrl, {
        cache: "no-store",
        credentials: "same-origin",
        headers: {
          Accept: "text/csv",
          ...(apiToken ? { Authorization: `Bearer ${apiToken}` } : {}),
        },
        method: "GET",
        signal: abortContext.signal,
      });
      if (!response.ok) {
        const payload = await responsePayload(response);
        const envelope = apiEnvelopeSchema.safeParse(payload);
        if (!envelope.success) {
          throw new ApiError({
            kind: "invalid_response",
            message: "服务响应格式无效，请检查服务版本。",
            status: response.status,
          });
        }
        throw new ApiError({
          code: envelope.data.code,
          kind: "http",
          message: envelope.data.message?.trim() || fallbackHttpMessage(response.status),
          status: response.status,
        });
      }
      return response.text();
    } catch (error) {
      if (isAbortError(error)) {
        throw error;
      }
      if (isApiError(error)) {
        throw error;
      }
      throw new ApiError({
        kind: "network",
        message: "无法连接到本地服务，请确认服务已启动。",
      });
    } finally {
      abortContext.cleanup();
    }
  }

  return {
    createAccount(input: CreateAccountInput, requestOptions?: ApiRequestOptions) {
      return request({
        body: {
          cookies: input.cookies,
          host: input.host,
          icloud_email: input.icloudEmail,
          name: input.name,
          proxy: input.proxy,
        },
        method: "POST",
        path: "accounts",
        responseSchema: accountSchema,
        signal: requestOptions?.signal,
      });
    },

    createAlias(input: CreateAliasInput, requestOptions?: ApiRequestOptions) {
      return request({
        body: {
          account_id: input.accountId,
          label: input.label,
        },
        method: "POST",
        path: "create",
        responseSchema: createdAliasSchema,
        signal: requestOptions?.signal,
      });
    },

    createAliasesBatch(
      accountId: string,
      input: CreateAliasesBatchInput,
      requestOptions?: ApiRequestOptions,
    ) {
      return request({
        body: {
          count: input.count,
          label_prefix: input.labelPrefix,
        },
        method: "POST",
        path: endpointPath("accounts", accountId, "aliases", "batch"),
        responseSchema: aliasBatchResultSchema,
        signal: requestOptions?.signal,
      });
    },

    deactivateAlias(accountId: string, aliasId: string, requestOptions?: ApiRequestOptions) {
      return request({
        body: { account_id: accountId },
        method: "POST",
        path: endpointPath("aliases", aliasId, "deactivate"),
        responseSchema: aliasActionSchema,
        signal: requestOptions?.signal,
      });
    },

    deleteAccount(accountId: string, requestOptions?: ApiRequestOptions) {
      return request({
        method: "DELETE",
        path: endpointPath("accounts", accountId),
        responseSchema: deletedAccountSchema,
        signal: requestOptions?.signal,
      });
    },

    deleteAlias(accountId: string, aliasId: string, requestOptions?: ApiRequestOptions) {
      return request({
        body: { account_id: accountId },
        method: "DELETE",
        path: endpointPath("aliases", aliasId),
        responseSchema: deletedAliasSchema,
        signal: requestOptions?.signal,
      });
    },

    downloadAliasCreationHistory(accountId: string, requestOptions?: ApiRequestOptions) {
      return requestText(
        endpointPath("accounts", accountId, "alias-creation-history.csv"),
        requestOptions?.signal,
      );
    },

    getHealth(requestOptions?: ApiRequestOptions) {
      return request({
        method: "GET",
        path: "health",
        responseSchema: healthSchema,
        signal: requestOptions?.signal,
      });
    },

    getEmailNotification(requestOptions?: ApiRequestOptions) {
      return request({
        method: "GET",
        path: "notifications/email",
        responseSchema: emailNotificationSchema,
        signal: requestOptions?.signal,
      });
    },

    getWebhookNotification(requestOptions?: ApiRequestOptions) {
      return request({
        method: "GET",
        path: "notifications/webhook",
        responseSchema: webhookNotificationSchema,
        signal: requestOptions?.signal,
      });
    },

    getPlatformAuthSession(requestOptions?: ApiRequestOptions) {
      return request({
        method: "GET",
        path: "auth/session",
        responseSchema: platformAuthStatusSchema,
        signal: requestOptions?.signal,
      });
    },

    listOperationLogs(requestOptions?: ApiRequestOptions) {
      return request({
        method: "GET",
        path: "logs",
        query: { limit: 200 },
        responseSchema: operationLogsSchema,
        signal: requestOptions?.signal,
      });
    },

    testEmailNotification(requestOptions?: ApiRequestOptions) {
      return request({
        method: "POST",
        path: "notifications/email/test",
        responseSchema: emailNotificationTestSchema,
        signal: requestOptions?.signal,
      });
    },

    testWebhookNotification(requestOptions?: ApiRequestOptions) {
      return request({
        method: "POST",
        path: "notifications/webhook/test",
        responseSchema: webhookNotificationTestSchema,
        signal: requestOptions?.signal,
      });
    },

    loginPlatform(input: PlatformAuthCredentials, requestOptions?: ApiRequestOptions) {
      return request({
        body: input,
        method: "POST",
        path: "auth/login",
        responseSchema: platformAuthStatusSchema,
        signal: requestOptions?.signal,
      });
    },

    logoutPlatform(requestOptions?: ApiRequestOptions) {
      return request({
        method: "POST",
        path: "auth/logout",
        responseSchema: platformAuthStatusSchema,
        signal: requestOptions?.signal,
      });
    },

    getAliasAutomation(accountId: string, requestOptions?: ApiRequestOptions) {
      return request({
        method: "GET",
        path: endpointPath("accounts", accountId, "alias-automation"),
        responseSchema: aliasAutomationSchema,
        signal: requestOptions?.signal,
      });
    },

    listAccounts(requestOptions?: ApiRequestOptions) {
      return request({
        method: "GET",
        path: "accounts",
        responseSchema: z.array(accountSchema),
        signal: requestOptions?.signal,
      });
    },

    listAliases(accountId: string, requestOptions?: ApiRequestOptions) {
      return request({
        method: "GET",
        path: "aliases",
        query: { account_id: accountId },
        responseSchema: aliasesSchema,
        signal: requestOptions?.signal,
      });
    },

    listAliasCreationHistory(accountId: string, requestOptions?: ApiRequestOptions) {
      return request({
        method: "GET",
        path: endpointPath("accounts", accountId, "alias-creation-history"),
        query: { limit: aliasCreationHistoryRequestLimit },
        responseSchema: aliasCreationHistorySchema,
        signal: requestOptions?.signal,
      });
    },

    listInbox(query: InboxQuery, requestOptions?: ApiRequestOptions) {
      return request({
        method: "GET",
        path: "inbox",
        query: {
          account_id: query.accountId,
          alias: query.alias,
          before_uid: query.beforeUid,
          days: query.days,
          include_preview: false,
          limit: query.limit,
        },
        responseSchema: inboxSchema,
        signal: requestOptions?.signal,
        timeoutMs: requestOptions?.timeoutMs,
      });
    },

    getInboxMessage(accountId: string, messageId: string, requestOptions?: ApiRequestOptions) {
      return request({
        method: "GET",
        path: endpointPath("inbox", "messages", messageId),
        query: { account_id: accountId },
        responseSchema: inboxMessageSchema,
        signal: requestOptions?.signal,
        timeoutMs: requestOptions?.timeoutMs,
      });
    },

    reactivateAlias(accountId: string, aliasId: string, requestOptions?: ApiRequestOptions) {
      return request({
        body: { account_id: accountId },
        method: "POST",
        path: endpointPath("aliases", aliasId, "reactivate"),
        responseSchema: aliasActionSchema,
        signal: requestOptions?.signal,
      });
    },

    pauseAliasAutomation(accountId: string, requestOptions?: ApiRequestOptions) {
      return request({
        method: "POST",
        path: endpointPath("accounts", accountId, "alias-automation", "pause"),
        responseSchema: aliasAutomationSchema,
        signal: requestOptions?.signal,
      });
    },

    resumeAliasAutomation(accountId: string, requestOptions?: ApiRequestOptions) {
      return request({
        method: "POST",
        path: endpointPath("accounts", accountId, "alias-automation", "resume"),
        responseSchema: aliasAutomationSchema,
        signal: requestOptions?.signal,
      });
    },

    runAliasAutomation(accountId: string, requestOptions?: ApiRequestOptions) {
      return request({
        method: "POST",
        path: endpointPath("accounts", accountId, "alias-automation", "run"),
        responseSchema: aliasAutomationRunSchema,
        signal: requestOptions?.signal,
      });
    },

    previewAliasAutomation(accountId: string, requestOptions?: ApiRequestOptions) {
      return request({
        method: "POST",
        path: endpointPath("accounts", accountId, "alias-automation", "preview"),
        responseSchema: aliasAutomationPreviewSchema,
        signal: requestOptions?.signal,
      });
    },

    reloadConfig(requestOptions?: ApiRequestOptions) {
      return request({
        method: "POST",
        path: "reload",
        responseSchema: reloadedConfigSchema,
        signal: requestOptions?.signal,
      });
    },

    setAppPassword(
      accountId: string,
      input: SetAppPasswordInput,
      requestOptions?: ApiRequestOptions,
    ) {
      return request({
        body: {
          app_password: input.appPassword,
          icloud_email: input.icloudEmail,
        },
        method: "POST",
        path: endpointPath("accounts", accountId, "password"),
        responseSchema: accountSchema,
        signal: requestOptions?.signal,
      });
    },

    setupPlatformAuth(input: PlatformAuthCredentials, requestOptions?: ApiRequestOptions) {
      return request({
        body: input,
        method: "POST",
        path: "auth/setup",
        responseSchema: platformAuthStatusSchema,
        signal: requestOptions?.signal,
      });
    },

    startLogin(accountId: string, password: string, requestOptions?: ApiRequestOptions) {
      return request({
        body: { password },
        method: "POST",
        path: endpointPath("accounts", accountId, "login", "start"),
        responseSchema: z.union([accountSchema, otpChallengeSchema]),
        signal: requestOptions?.signal,
      });
    },

    updateCookies(
      accountId: string,
      cookies: Record<string, string>,
      requestOptions?: ApiRequestOptions,
    ) {
      return request({
        body: { cookies },
        method: "PUT",
        path: endpointPath("accounts", accountId, "cookies"),
        responseSchema: accountSchema,
        signal: requestOptions?.signal,
      });
    },

    updateEmailNotification(input: EmailNotificationInput, requestOptions?: ApiRequestOptions) {
      return request({
        body: {
          authorization_code: input.authorizationCode,
          enabled: input.enabled,
          sender_email: input.senderEmail,
          recipient_email: input.recipientEmail,
        },
        method: "PUT",
        path: "notifications/email",
        responseSchema: emailNotificationSchema,
        signal: requestOptions?.signal,
      });
    },

    updateWebhookNotification(input: WebhookNotificationInput, requestOptions?: ApiRequestOptions) {
      return request({
        body: {
          enabled: input.enabled,
          secret: input.secret,
          url: input.url,
        },
        method: "PUT",
        path: "notifications/webhook",
        responseSchema: webhookNotificationSchema,
        signal: requestOptions?.signal,
      });
    },

    updateAliasAutomation(
      accountId: string,
      input: AliasAutomationInput,
      requestOptions?: ApiRequestOptions,
    ) {
      return request({
        body: {
          enabled: input.enabled,
          interval_minutes: input.intervalMinutes,
          allowed_weekdays: input.allowedWeekdays ?? [],
          execution_window_start: input.executionWindowStart ?? "",
          execution_window_end: input.executionWindowEnd ?? "",
          scheduled_batch_size: input.scheduledBatchSize,
          minimum_active: input.minimumActive,
          target_active: input.targetActive,
          max_batch_size: input.maxBatchSize,
          max_total_aliases: input.maxTotalAliases,
          max_failure_count: input.maxFailureCount,
          daily_creation_limit: input.dailyCreationLimit,
          target_created: input.targetCreated,
          label_prefix: input.labelPrefix,
        },
        method: "PUT",
        path: endpointPath("accounts", accountId, "alias-automation"),
        responseSchema: aliasAutomationSchema,
        signal: requestOptions?.signal,
      });
    },

    verifyLogin(
      accountId: string,
      challengeId: string,
      otpCode: string,
      requestOptions?: ApiRequestOptions,
    ) {
      return request({
        body: {
          challenge_id: challengeId,
          otp_code: otpCode,
        },
        method: "POST",
        path: endpointPath("accounts", accountId, "login", "verify"),
        responseSchema: accountSchema,
        signal: requestOptions?.signal,
      });
    },
  };
}

export const api = createApiClient();

export type ApiClient = ReturnType<typeof createApiClient>;
export type {
  Account,
  AliasAction,
  AliasCreationHistory,
  AliasAutomation,
  AliasAutomationPreview,
  AliasAutomationRun,
  AliasBatchResult,
  Aliases,
  CreatedAlias,
  DeletedAccount,
  DeletedAlias,
  Health,
  Inbox,
  InboxMessage,
  EmailNotification,
  EmailNotificationTestResult,
  PlatformAuthStatus,
  ReloadedConfig,
  WebhookNotification,
  WebhookNotificationTestResult,
};
