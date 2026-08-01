import { z } from "zod";

export const appleLoginSchema = z.object({
  password: z
    .string()
    .max(1024, "Apple ID 密码不能超过 1024 个字符")
    .refine((value) => value.trim().length > 0, "请输入 Apple ID 密码"),
});

export const otpCodeSchema = z.object({
  otpCode: z
    .string()
    .trim()
    .regex(/^\d{6}$/, "请输入 6 位数字验证码"),
});

export type AppleLoginFormValues = z.infer<typeof appleLoginSchema>;
export type OtpCodeFormValues = z.infer<typeof otpCodeSchema>;
