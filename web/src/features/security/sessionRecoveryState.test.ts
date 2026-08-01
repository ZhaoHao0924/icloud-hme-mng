import { describe, expect, it } from "vitest";

import {
  createSessionRecoveryLocationState,
  isStoredSessionExpiredError,
  readSessionRecoveryLocationState,
} from "./sessionRecoveryState";

describe("session recovery location state", () => {
  it("round-trips a safe internal source path", () => {
    const state = createSessionRecoveryLocationState(
      "/accounts/acc_main/inbox?alias=owner%40icloud.com",
    );

    expect(readSessionRecoveryLocationState(state)).toEqual({
      from: "/accounts/acc_main/inbox?alias=owner%40icloud.com",
      reason: "icloud_session_expired",
    });
  });

  it.each([
    null,
    {},
    { sessionRecovery: { from: "https://example.com", reason: "icloud_session_expired" } },
    { sessionRecovery: { from: "//example.com", reason: "icloud_session_expired" } },
    { sessionRecovery: { from: "/accounts", reason: "unknown" } },
  ])("rejects malformed or external recovery state", (state) => {
    expect(readSessionRecoveryLocationState(state)).toBeNull();
  });
});

describe("stored session errors", () => {
  it.each([
    "Cookie 会话已过期，请更新凭据",
    "iCloud 会话失效,请更新 Cookie",
    "Cookie 校验失败: HTTP 401",
    "cookie request returned 403",
    "Session expired",
  ])("recognizes %s", (message) => {
    expect(isStoredSessionExpiredError(message)).toBe(true);
  });

  it.each(["", "Cookie 格式无效", "IMAP 连接超时", "Apple 服务返回 502"])(
    "does not misclassify %s",
    (message) => {
      expect(isStoredSessionExpiredError(message)).toBe(false);
    },
  );
});
