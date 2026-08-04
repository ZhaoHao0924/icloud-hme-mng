import { expect, test } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.route("**/api/auth/session", (route) =>
    route.fulfill({
      body: JSON.stringify({
        data: {
          authenticated: true,
          configured: true,
          expires_at: "2099-01-01T00:00:00Z",
          username: "e2e-admin",
        },
        success: true,
      }),
      contentType: "application/json",
      status: 200,
    }),
  );
  await page.route("**/api/accounts", (route) =>
    route.fulfill({
      body: JSON.stringify({ data: [], success: true }),
      contentType: "application/json",
      status: 200,
    }),
  );
  await page.route("**/api/aliases**", (route) => {
    const accountId = new URL(route.request().url()).searchParams.get("account_id") ?? "";
    return route.fulfill({
      body: JSON.stringify({
        data: { account_id: accountId, aliases: [], count: 0 },
        success: true,
      }),
      contentType: "application/json",
      status: 200,
    });
  });
  await page.route("**/api/inbox**", (route) => {
    const url = new URL(route.request().url());
    return route.fulfill({
      body: JSON.stringify({
        data: {
          account_id: url.searchParams.get("account_id") ?? "",
          alias: url.searchParams.get("alias") ?? "",
          count: 0,
          messages: [],
          method: "imap",
        },
        success: true,
      }),
      contentType: "application/json",
      status: 200,
    });
  });
  await page.route("**/api/health", (route) =>
    route.fulfill({
      body: JSON.stringify({
        data: {
          config_available: true,
          service: "icloud-hme",
          status: "ok",
          version: "smoke-test",
        },
        success: true,
      }),
      contentType: "application/json",
      status: 200,
    }),
  );
  await page.route("**/api/logs*", (route) =>
    route.fulfill({
      body: JSON.stringify({
        data: { count: 0, entries: [], retention_days: 7 },
        success: true,
      }),
      contentType: "application/json",
      status: 200,
    }),
  );
  await page.route("**/api/notifications/email", (route) =>
    route.fulfill({
      body: JSON.stringify({
        data: {
          configured: false,
          enabled: false,
          provider: "163",
          recipient_email: "",
          sender_email: "",
          smtp_host: "smtp.163.com",
          smtp_port: 465,
        },
        success: true,
      }),
      contentType: "application/json",
      status: 200,
    }),
  );
  await page.route("**/api/notifications/webhook", (route) =>
    route.fulfill({
      body: JSON.stringify({
        data: { configured: false, enabled: false, url: "" },
        success: true,
      }),
      contentType: "application/json",
      status: 200,
    }),
  );
  await page.route("**/api/reload", (route) =>
    route.fulfill({
      body: JSON.stringify({ data: { message: "配置已重新加载" }, success: true }),
      contentType: "application/json",
      status: 200,
    }),
  );
});

test("account workspace loads", async ({ page }) => {
  await page.goto("/accounts");

  await expect(page).toHaveTitle("iCloud HME");
  await expect(page.getByRole("heading", { level: 1, name: "账户" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 3, name: "暂无账户" })).toBeVisible();
});

test("API token access is retried in memory without browser persistence", async ({ page }) => {
  const apiToken = "fe306-browser-api-token";
  const accountAuthorization: Array<string | undefined> = [];
  const healthAuthorization: Array<string | undefined> = [];

  await page.unroute("**/api/accounts");
  await page.unroute("**/api/health");
  await page.route("**/api/accounts", (route) => {
    const authorization = route.request().headers().authorization;
    accountAuthorization.push(authorization);
    if (authorization !== `Bearer ${apiToken}`) {
      return route.fulfill({
        body: JSON.stringify({
          code: "api_token_invalid",
          message: "API 访问令牌无效或缺失",
          success: false,
        }),
        contentType: "application/json",
        status: 401,
      });
    }
    return route.fulfill({
      body: JSON.stringify({
        data: [
          {
            alias_active: 0,
            alias_total: 0,
            created_at: "2026-08-01T08:00:00+08:00",
            has_app_password: false,
            has_cookies: false,
            host: "icloud.com",
            icloud_email: "remote@icloud.com",
            id: "remote",
            last_error: "",
            last_validated: "",
            name: "远程账户",
            proxy_configured: false,
            real_email: "",
            status: "pending",
          },
        ],
        success: true,
      }),
      contentType: "application/json",
      status: 200,
    });
  });
  await page.route("**/api/health", (route) => {
    const authorization = route.request().headers().authorization;
    healthAuthorization.push(authorization);
    if (authorization !== `Bearer ${apiToken}`) {
      return route.fulfill({
        body: JSON.stringify({
          code: "api_token_invalid",
          message: "API 访问令牌无效或缺失",
          success: false,
        }),
        contentType: "application/json",
        status: 401,
      });
    }
    return route.fulfill({
      body: JSON.stringify({
        data: {
          config_available: true,
          service: "icloud-hme",
          status: "ok",
          version: "smoke-test",
        },
        success: true,
      }),
      contentType: "application/json",
      status: 200,
    });
  });

  const consoleMessages: string[] = [];
  page.on("console", (message) => consoleMessages.push(message.text()));
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/accounts");

  const dialog = page.getByRole("dialog", { name: "API 访问令牌" });
  const input = dialog.getByRole("textbox", { name: "API 访问令牌" });
  await expect(dialog).toBeVisible();
  const dimensions = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: window.innerWidth,
  }));
  expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
  await expect(input).toHaveAttribute("type", "password");
  await input.fill(apiToken);
  await dialog.getByRole("button", { name: "验证并继续" }).click();

  await expect(page.getByText("远程账户")).toBeVisible();
  expect(accountAuthorization).toContain(`Bearer ${apiToken}`);
  expect(healthAuthorization).toEqual([`Bearer ${apiToken}`]);

  const persistence = await page.evaluate(async () => {
    function storageEntries(storage: Storage) {
      return Object.fromEntries(
        Array.from({ length: storage.length }, (_, index) => {
          const key = storage.key(index) ?? "";
          return [key, storage.getItem(key)];
        }),
      );
    }

    const cacheEntries: Array<{ request: string; response: string }> = [];
    for (const cacheName of await caches.keys()) {
      const cache = await caches.open(cacheName);
      for (const request of await cache.keys()) {
        const response = await cache.match(request);
        cacheEntries.push({
          request: request.url,
          response: response ? await response.clone().text() : "",
        });
      }
    }

    const indexedDatabases =
      typeof indexedDB.databases === "function"
        ? (await indexedDB.databases()).map(({ name, version }) => ({ name, version }))
        : [];

    return {
      cacheEntries,
      document: document.documentElement.outerHTML,
      indexedDatabases,
      localStorage: storageEntries(localStorage),
      resourceURLs: performance.getEntriesByType("resource").map((entry) => entry.name),
      sessionStorage: storageEntries(sessionStorage),
      url: window.location.href,
    };
  });
  expect(JSON.stringify({ consoleMessages, persistence })).not.toContain(apiToken);

  await page.reload();
  await expect(page.getByRole("dialog", { name: "API 访问令牌" })).toBeVisible();
});

test("lazy navigation keeps the current page visible until its module loads", async ({ page }) => {
  let delaySettingsModule = false;
  let releaseModule = () => {};
  const moduleGate = new Promise<void>((resolve) => {
    releaseModule = resolve;
  });
  let markModuleRequested = () => {};
  const moduleRequested = new Promise<void>((resolve) => {
    markModuleRequested = resolve;
  });
  const settingsModulePattern = "**/src/features/system/SettingsPage.tsx*";

  await page.route(settingsModulePattern, async (route) => {
    if (!delaySettingsModule) {
      await route.continue();
      return;
    }

    markModuleRequested();
    await moduleGate;
    await route.continue();
  });

  try {
    await page.goto("/accounts");
    const emptyState = page.getByRole("heading", { level: 3, name: "暂无账户" });
    const content = page.locator("main.content");
    await expect(emptyState).toBeVisible();

    delaySettingsModule = true;
    await page.getByRole("link", { name: "设置", exact: true }).click();
    await moduleRequested;

    await expect(content).toHaveAttribute("aria-busy", "true");
    await expect(emptyState).toBeVisible();

    releaseModule();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.getByText("icloud-hme")).toBeVisible();
    await expect(content).not.toHaveAttribute("aria-busy");
  } finally {
    releaseModule();
    await page.unroute(settingsModulePattern);
  }
});

test("lazy module failures expose a route recovery path", async ({ page }) => {
  const settingsModulePattern = "**/src/features/system/SettingsPage.tsx*";

  await page.route(settingsModulePattern, (route) =>
    route.fulfill({
      body: "Settings module unavailable",
      contentType: "text/plain",
      status: 503,
    }),
  );

  try {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/settings");

    await expect(page.getByRole("heading", { level: 1, name: "页面加载失败" })).toBeVisible();
    await expect(page.getByRole("heading", { level: 1, name: "页面加载失败" })).toBeFocused();
    const dimensions = await page.evaluate(() => ({
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
    }));
    expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
    await page.getByRole("link", { name: "返回账户" }).click();
    await expect(page).toHaveURL(/\/accounts$/);
    await expect(page.getByRole("heading", { level: 1, name: "账户" })).toBeVisible();
  } finally {
    await page.unroute(settingsModulePattern);
  }
});

test("lazy module failures can reload updated resources", async ({ page }) => {
  let failFirstSettingsModuleRequest = true;
  const settingsModulePattern = "**/src/features/system/SettingsPage.tsx*";

  await page.route(settingsModulePattern, (route) => {
    if (!failFirstSettingsModuleRequest) {
      return route.continue();
    }

    failFirstSettingsModuleRequest = false;
    return route.fulfill({
      body: "Settings module unavailable",
      contentType: "text/plain",
      status: 503,
    });
  });

  try {
    await page.goto("/settings");
    await expect(page.getByRole("heading", { level: 1, name: "页面加载失败" })).toBeVisible();

    await page.getByRole("button", { name: "重新加载页面" }).click();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.getByRole("heading", { level: 1, name: "系统设置" })).toBeVisible();
  } finally {
    await page.unroute(settingsModulePattern);
  }
});

test("account workspace fits the responsive baselines", async ({ page }) => {
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    await page.goto("/accounts");

    const dimensions = await page.evaluate(() => ({
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
    }));
    expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
    await expect(page.getByRole("heading", { level: 1, name: "账户" })).toBeVisible();
  }
});

test("planned routes and not-found handling are reachable", async ({ page }) => {
  await page.route("**/api/accounts", (route) =>
    route.fulfill({
      body: JSON.stringify({
        data: [
          {
            alias_active: 0,
            alias_total: 0,
            created_at: "2026-08-01T08:00:00+08:00",
            has_app_password: false,
            has_cookies: false,
            host: "icloud.com",
            icloud_email: "demo@icloud.com",
            id: "demo",
            last_error: "",
            last_validated: "",
            name: "演示账户",
            proxy_configured: false,
            real_email: "",
            status: "pending",
          },
        ],
        success: true,
      }),
      contentType: "application/json",
      status: 200,
    }),
  );

  const routes = [
    ["/accounts/demo/aliases", "别名"],
    ["/accounts/demo/inbox", "收件箱"],
    ["/accounts/demo/security", "凭据"],
    ["/settings", "系统设置"],
  ] as const;

  for (const [path, heading] of routes) {
    await page.goto(path);
    await expect(page.getByRole("heading", { level: 1, name: heading })).toBeVisible();
    if (path.endsWith("/security")) {
      await expect(page.getByRole("heading", { level: 3, name: "Cookie" })).toBeVisible();
    } else if (path.endsWith("/aliases")) {
      await expect(page.getByRole("heading", { level: 3, name: "暂无别名" })).toBeVisible();
    } else if (path.endsWith("/inbox")) {
      await expect(page.getByLabel("账户", { exact: true })).toHaveValue("demo@icloud.com");
      await expect(page.getByLabel("别名", { exact: true })).toHaveValue("");
      await expect(page.getByText("暂无匹配邮件")).toBeVisible();
    } else if (path === "/settings") {
      await expect(page.getByText("icloud-hme")).toBeVisible();
      await expect(page.getByText("正常")).toBeVisible();
    } else {
      await expect(page.getByText("暂无数据")).toBeVisible();
    }
  }

  await page.goto("/route-that-does-not-exist");
  await expect(page.getByRole("heading", { level: 2, name: "找不到页面" })).toBeVisible();
  await expect(page.getByRole("link", { name: "返回账户" })).toBeVisible();
});
