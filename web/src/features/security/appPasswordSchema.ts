import { z } from "zod";

const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export const appPasswordSchema = z.object({
  appPassword: z
    .string()
    .trim()
    .min(1, "请输入 App 专用密码")
    .max(256, "App 专用密码不能超过 256 个字符"),
  icloudEmail: z
    .string()
    .trim()
    .min(1, "请输入 iCloud 邮箱")
    .max(320, "iCloud 邮箱不能超过 320 个字符")
    .refine((value) => emailPattern.test(value), {
      message: "请输入有效的 iCloud 邮箱",
    }),
});

export type AppPasswordFormValues = z.infer<typeof appPasswordSchema>;
