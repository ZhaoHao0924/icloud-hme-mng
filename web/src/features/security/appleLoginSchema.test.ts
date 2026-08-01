import { describe, expect, it } from "vitest";

import { appleLoginSchema, otpCodeSchema } from "./appleLoginSchema";

describe("appleLoginSchema", () => {
  it("keeps a valid Apple ID password unchanged", () => {
    expect(appleLoginSchema.parse({ password: "  exact password  " })).toEqual({
      password: "  exact password  ",
    });
  });

  it("rejects blank and oversized passwords", () => {
    const blank = appleLoginSchema.safeParse({ password: "   " });
    const oversized = appleLoginSchema.safeParse({ password: "x".repeat(1025) });

    expect(blank.success).toBe(false);
    if (!blank.success) {
      expect(blank.error.flatten().fieldErrors.password).toEqual(["请输入 Apple ID 密码"]);
    }
    expect(oversized.success).toBe(false);
    if (!oversized.success) {
      expect(oversized.error.flatten().fieldErrors.password).toEqual([
        "Apple ID 密码不能超过 1024 个字符",
      ]);
    }
  });
});

describe("otpCodeSchema", () => {
  it("accepts exactly six ASCII digits and trims surrounding whitespace", () => {
    expect(otpCodeSchema.parse({ otpCode: " 123456 " })).toEqual({ otpCode: "123456" });
  });

  it.each(["", "12345", "1234567", "12a456", "１２３４５６"])(
    "rejects invalid OTP value %j",
    (otpCode) => {
      const result = otpCodeSchema.safeParse({ otpCode });

      expect(result.success).toBe(false);
      if (!result.success) {
        expect(result.error.flatten().fieldErrors.otpCode).toEqual(["请输入 6 位数字验证码"]);
      }
    },
  );
});
