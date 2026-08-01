import { describe, expect, it } from "vitest";

import { addAccountSchema } from "./addAccountSchema";

describe("addAccountSchema", () => {
  it("accepts supported Apple email domains and proxy schemes", () => {
    expect(
      addAccountSchema.parse({
        host: "icloud.com.cn",
        icloudEmail: " Owner@ME.com ",
        name: " 中国区账户 ",
        proxy: "socks5://127.0.0.1:1080",
      }),
    ).toEqual({
      host: "icloud.com.cn",
      icloudEmail: "Owner@ME.com",
      name: "中国区账户",
      proxy: "socks5://127.0.0.1:1080",
    });
  });

  it("rejects missing or unsupported account fields", () => {
    const result = addAccountSchema.safeParse({
      host: "icloud.com",
      icloudEmail: "owner@example.com",
      name: "",
      proxy: "ftp://127.0.0.1",
    });

    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.error.flatten().fieldErrors).toMatchObject({
        icloudEmail: ["请输入有效的 iCloud、me.com 或 mac.com 邮箱"],
        name: ["请输入账户名称"],
        proxy: ["请输入有效的 HTTP、HTTPS 或 SOCKS5 代理地址"],
      });
    }
  });
});
