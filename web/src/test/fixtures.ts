import type { Account, Alias, Health, InboxMessage, OtpChallenge } from "../api/schemas";

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

export const otpChallengeFixture: OtpChallenge = {
  challenge_id: "mock-login-challenge",
  expires_in: 300,
  status: "otp_required",
};
