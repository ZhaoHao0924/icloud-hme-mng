import { describe, expect, it } from "vitest";

import { appPasswordSchema } from "./appPasswordSchema";

describe("appPasswordSchema", () => {
  it("normalizes valid credential fields", () => {
    expect(
      appPasswordSchema.parse({
        appPassword: "  abcd-efgh-ijkl-mnop  ",
        icloudEmail: "  owner@icloud.com.cn  ",
      }),
    ).toEqual({
      appPassword: "abcd-efgh-ijkl-mnop",
      icloudEmail: "owner@icloud.com.cn",
    });
  });

  it("rejects empty passwords and malformed email addresses", () => {
    const result = appPasswordSchema.safeParse({
      appPassword: "   ",
      icloudEmail: "not-an-email",
    });

    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.error.flatten().fieldErrors).toMatchObject({
        appPassword: ["请输入 App 专用密码"],
        icloudEmail: ["请输入有效的 iCloud 邮箱"],
      });
    }
  });
});
