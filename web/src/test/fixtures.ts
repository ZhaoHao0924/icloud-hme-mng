import type {
  Account,
  Alias,
  EmailNotification,
  Health,
  InboxMessage,
  OperationLogs,
  OtpChallenge,
  WebhookNotification,
} from "../api/schemas";

export const accountFixtures: Account[] = [
  {
    alias_active: 8,
    alias_total: 10,
    created_at: "2026-07-31T09:00:00+08:00",
    has_app_password: true,
    has_cookies: true,
    host: "icloud.com",
    icloud_email: "primary@icloud.com",
    id: "acc_primary",
    last_error: "",
    last_validated: "2026-08-01T08:30:00+08:00",
    name: "主账号",
    proxy_configured: false,
    real_email: "primary@example.com",
    status: "active",
  },
  {
    alias_active: 0,
    alias_total: 0,
    created_at: "2026-08-01T08:00:00+08:00",
    has_app_password: false,
    has_cookies: false,
    host: "icloud.com.cn",
    icloud_email: "pending@icloud.com.cn",
    id: "acc_pending",
    last_error: "",
    last_validated: "",
    name: "待登录账号",
    proxy_configured: true,
    real_email: "",
    status: "pending",
  },
];

export const errorAccountFixture: Account = {
  alias_active: 4,
  alias_total: 6,
  created_at: "2026-07-29T16:00:00+08:00",
  has_app_password: false,
  has_cookies: true,
  host: "icloud.com",
  icloud_email: "recover@icloud.com",
  id: "acc_error",
  last_error: "Cookie 会话已过期，请更新凭据",
  last_validated: "2026-07-31T22:10:00+08:00",
  name: "需要恢复的账号",
  proxy_configured: false,
  real_email: "recover@example.com",
  status: "error",
};

export const mixedAccountFixtures: Account[] = [...accountFixtures, errorAccountFixture];

export const aliasFixtures: Alias[] = [
  {
    active: true,
    anonymousId: "alias_active_1",
    createdAt: "2026-07-30T10:00:00Z",
    email: "quiet-orchid@icloud.com",
    label: "GitHub",
  },
  {
    active: false,
    anonymousId: "alias_inactive_1",
    createdAt: "2026-07-28T09:30:00Z",
    email: "silver-field@icloud.com",
    label: "旧服务",
  },
];

export const inboxMessageFixtures: InboxMessage[] = [
  {
    date: "2026-08-01T08:10:00+08:00",
    from: "GitHub <noreply@github.com>",
    id: "1042",
    preview: "请确认你的登录操作。",
    subject: "登录确认",
    to: "quiet-orchid@icloud.com",
  },
  {
    date: "2026-08-01T07:42:00+08:00",
    from: "Apple <no_reply@email.apple.com>",
    id: "1041",
    preview: "你的 Apple ID 刚刚在新设备上完成登录。如果这不是你本人操作，请立即检查账户安全设置。",
    subject: "新设备登录提醒",
    to: "silver-field@icloud.com",
  },
  {
    date: "2026-07-31T22:18:00+08:00",
    from: "Linear <notifications@linear.app>",
    id: "1040",
    preview: "你关注的问题状态已更新，相关讨论和后续通知会继续发送到这个 Hide My Email 地址。",
    subject: "问题状态已更新",
    to: "quiet-orchid@icloud.com",
  },
];

const longInboxToken = "unbroken-inbox-content-".repeat(24);

export const inboxLongMessageFixtures: InboxMessage[] = [
  {
    date: "2026-08-01T08:10:00+08:00",
    from: `LongSender-${longInboxToken}@example.test`,
    id: "long-1042",
    preview: Array.from(
      { length: 18 },
      (_, index) => `Preview line ${index + 1}: ${longInboxToken}`,
    ).join("\n"),
    subject: `LongSubject-${longInboxToken}`,
    to: `long-recipient-${longInboxToken}@icloud.com`,
  },
];

export const healthyServiceFixture: Health = {
  config_available: true,
  service: "icloud-hme",
  status: "ok",
  version: "dev",
};

export const degradedServiceFixture: Health = {
  config_available: false,
  service: "icloud-hme",
  status: "degraded",
  version: "dev",
};

export const operationLogsFixture: OperationLogs = {
  count: 3,
  entries: [
    {
      duration_ms: 842,
      error_code: "",
      level: "info",
      operation: "读取收件箱",
      operation_type: "inbox",
      request: {
        alias_filter_applied: false,
        body: { content_type: "", encoding: "", present: false, value: "" },
        body_present: false,
        method: "GET",
        pagination_requested: true,
        path: "/api/inbox",
        path_params: {},
        raw_query: "account_id=demo-account&limit=20&days=7",
        source: "api",
      },
      request_id: "0f5f0fcb2a914b9a8fda31de17d8ee01",
      response: {
        body: {
          content_type: "application/json; charset=utf-8",
          encoding: "utf8",
          present: true,
          value: '{"success":true,"data":{"messages":[]}}',
        },
        created_count: 0,
        failed_count: 0,
        success: true,
      },
      retry_count: 0,
      schema_version: 3,
      status: 200,
      timestamp: "2026-08-02T08:30:00Z",
    },
    {
      duration_ms: 64,
      error_code: "unauthorized",
      level: "warning",
      operation: "更新 Cookie",
      operation_type: "accounts_id_cookies",
      request: {
        alias_filter_applied: false,
        body: {
          content_type: "application/json",
          encoding: "utf8",
          present: true,
          value: '{"cookies":"session=demo-cookie"}',
        },
        body_present: true,
        method: "PUT",
        pagination_requested: false,
        path: "/api/accounts/demo-account/cookies",
        path_params: { id: "demo-account" },
        raw_query: "",
        source: "api",
      },
      request_id: "7e0051e4d2f24ac19de311ca8792f301",
      response: {
        body: {
          content_type: "application/json; charset=utf-8",
          encoding: "utf8",
          present: true,
          value: '{"success":false,"message":"Cookie 已失效"}',
        },
        created_count: 0,
        failed_count: 0,
        success: false,
      },
      retry_count: 0,
      schema_version: 3,
      status: 401,
      timestamp: "2026-08-02T08:10:00Z",
    },
    {
      duration_ms: 2_541,
      error_code: "upstream_rejected",
      level: "error",
      operation: "批量创建别名",
      operation_type: "accounts_id_aliases_batch",
      request: {
        alias_filter_applied: false,
        body: {
          content_type: "application/json",
          encoding: "utf8",
          present: true,
          value: '{"count":3,"label_prefix":"demo"}',
        },
        body_present: true,
        method: "POST",
        pagination_requested: false,
        path: "/api/accounts/demo-account/aliases/batch",
        path_params: { id: "demo-account" },
        raw_query: "",
        source: "api",
      },
      request_id: "62f5b6d32ead4955bd408abc0f747762",
      response: {
        body: {
          content_type: "application/json; charset=utf-8",
          encoding: "utf8",
          present: true,
          value: '{"success":false,"message":"上游拒绝请求"}',
        },
        created_count: 0,
        failed_count: 0,
        success: false,
      },
      retry_count: 0,
      schema_version: 3,
      status: 502,
      timestamp: "2026-08-02T07:55:00Z",
    },
  ],
  retention_days: 7,
};

export const emailNotificationFixture: EmailNotification = {
  configured: false,
  enabled: false,
  provider: "163",
  sender_email: "",
  recipient_email: "",
  smtp_host: "smtp.163.com",
  smtp_port: 465,
};

export const webhookNotificationFixture: WebhookNotification = {
  configured: false,
  enabled: false,
  url: "",
};

export const otpChallengeFixture: OtpChallenge = {
  challenge_id: "mock-login-challenge",
  expires_in: 300,
  status: "otp_required",
};
