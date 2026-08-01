import { z } from "zod";

const icloudEmailPattern = /^[^\s@]+@(icloud\.com|me\.com|mac\.com)$/i;

export const addAccountSchema = z.object({
  host: z.enum(["icloud.com", "icloud.com.cn"]),
  icloudEmail: z
    .string()
    .trim()
    .min(1, "请输入 iCloud 邮箱")
    .refine((value) => icloudEmailPattern.test(value), {
      message: "请输入有效的 iCloud、me.com 或 mac.com 邮箱",
    }),
  name: z.string().trim().min(1, "请输入账户名称").max(100, "账户名称不能超过 100 个字符"),
  proxy: z
    .string()
    .trim()
    .refine((value) => {
      if (value === "") return true;
      try {
        const url = new URL(value);
        return ["http:", "https:", "socks5:"].includes(url.protocol) && url.hostname !== "";
      } catch {
        return false;
      }
    }, "请输入有效的 HTTP、HTTPS 或 SOCKS5 代理地址"),
});

export type AddAccountValues = z.infer<typeof addAccountSchema>;
