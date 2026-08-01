import { describe, expect, it } from "vitest";

import { CookieInputError, maxCookieCount, parseCookieInput } from "./cookieInput";

describe("parseCookieInput", () => {
  it("parses Cookie header strings and preserves equals signs in values", () => {
    expect(parseCookieInput("Cookie: session=abc==; user = 42; Secure")).toEqual({
      session: "abc==",
      user: "42",
    });
  });

  it("parses JSON maps", () => {
    expect(parseCookieInput('{"X-APPLE-TOKEN":"token","empty":""}')).toEqual({
      "X-APPLE-TOKEN": "token",
      empty: "",
    });
  });

  it("parses browser-exported Cookie arrays and ignores metadata", () => {
    expect(
      parseCookieInput(
        JSON.stringify([
          { domain: ".icloud.com", name: "token", path: "/", value: "first" },
          { domain: "www.icloud.com", name: "token", path: "/", value: "latest" },
          { name: "user", value: "owner" },
        ]),
      ),
    ).toEqual({ token: "latest", user: "owner" });
  });

  it.each([
    ["", "请输入 Cookie"],
    ["{invalid", "Cookie JSON 格式无效"],
    ['{"token":1}', "值必须是字符串"],
    ['[{"name":"token"}]', "缺少名称或值"],
    ['{"bad name":"value"}', "Cookie 名称格式无效"],
    ['{"token":"line\\nbreak"}', "Cookie 值不能包含换行符"],
  ])("rejects invalid input %#", (raw, message) => {
    expect(() => parseCookieInput(raw)).toThrow(message);
  });

  it("rejects Cookie maps above the server limit", () => {
    const cookies = Object.fromEntries(
      Array.from({ length: maxCookieCount + 1 }, (_, index) => [`cookie_${index}`, "value"]),
    );

    expect(() => parseCookieInput(JSON.stringify(cookies))).toThrow(CookieInputError);
    expect(() => parseCookieInput(JSON.stringify(cookies))).toThrow(
      `Cookie 数量不能超过 ${maxCookieCount} 个`,
    );
  });
});
