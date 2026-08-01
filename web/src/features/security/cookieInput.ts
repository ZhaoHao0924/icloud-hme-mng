export const maxCookieCount = 128;

export class CookieInputError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "CookieInputError";
  }
}

function addCookie(cookies: Map<string, string>, rawName: string, value: string) {
  const name = rawName.trim();
  if (!name || !/^[^\s=;,]+$/u.test(name)) {
    throw new CookieInputError("Cookie 名称格式无效");
  }
  if (/[\r\n]/u.test(value)) {
    throw new CookieInputError("Cookie 值不能包含换行符");
  }
  cookies.set(name, value);
}

function finishCookies(cookies: Map<string, string>) {
  if (cookies.size === 0) {
    throw new CookieInputError("未找到可用的 Cookie");
  }
  if (cookies.size > maxCookieCount) {
    throw new CookieInputError(`Cookie 数量不能超过 ${maxCookieCount} 个`);
  }
  return Object.fromEntries(cookies);
}

function parseJsonCookies(raw: string) {
  let input: unknown;
  try {
    input = JSON.parse(raw);
  } catch {
    throw new CookieInputError("Cookie JSON 格式无效");
  }

  const cookies = new Map<string, string>();
  if (Array.isArray(input)) {
    input.forEach((entry, index) => {
      if (typeof entry !== "object" || entry === null || Array.isArray(entry)) {
        throw new CookieInputError(`第 ${index + 1} 个 Cookie 条目格式无效`);
      }
      const { name, value } = entry as { name?: unknown; value?: unknown };
      if (typeof name !== "string" || typeof value !== "string") {
        throw new CookieInputError(`第 ${index + 1} 个 Cookie 条目缺少名称或值`);
      }
      addCookie(cookies, name, value);
    });
    return finishCookies(cookies);
  }

  if (typeof input !== "object" || input === null) {
    throw new CookieInputError("Cookie JSON 必须是对象或浏览器导出数组");
  }

  for (const [name, value] of Object.entries(input)) {
    if (typeof value !== "string") {
      throw new CookieInputError(`Cookie“${name}”的值必须是字符串`);
    }
    addCookie(cookies, name, value);
  }
  return finishCookies(cookies);
}

function parseHeaderCookies(raw: string) {
  const cookies = new Map<string, string>();
  const header = raw.replace(/^cookie\s*:\s*/iu, "");

  for (const rawPart of header.split(";")) {
    const part = rawPart.trim();
    if (!part) continue;
    const separatorIndex = part.indexOf("=");
    if (separatorIndex <= 0) continue;
    addCookie(cookies, part.slice(0, separatorIndex), part.slice(separatorIndex + 1).trim());
  }
  return finishCookies(cookies);
}

export function parseCookieInput(raw: string) {
  const input = raw.trim();
  if (!input) {
    throw new CookieInputError("请输入 Cookie");
  }
  if (input.startsWith("{") || input.startsWith("[")) {
    return parseJsonCookies(input);
  }
  return parseHeaderCookies(input);
}
