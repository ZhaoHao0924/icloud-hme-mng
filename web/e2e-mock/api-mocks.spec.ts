import { expect, test } from "@playwright/test";

type ApiEnvelope<T> = {
  data?: T;
  message?: string;
  success: boolean;
};

async function waitForMockWorker(page: import("@playwright/test").Page, pageTitle = "账户") {
  await expect(page.getByRole("heading", { level: 1, name: pageTitle })).toBeVisible();
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);
}

async function browserPersistenceSnapshot(page: import("@playwright/test").Page) {
  return page.evaluate(async () => {
    function storageEntries(storage: Storage) {
      return Object.fromEntries(
        Array.from({ length: storage.length }, (_, index) => {
          const key = storage.key(index) ?? "";
          return [key, storage.getItem(key)];
        }),
      );
    }

    const cacheEntries: Array<{ cache: string; request: string; response: string }> = [];
    for (const cacheName of await caches.keys()) {
      const cache = await caches.open(cacheName);
      for (const request of await cache.keys()) {
        const response = await cache.match(request);
        cacheEntries.push({
          cache: cacheName,
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
}

test("browser worker serves success fixtures", async ({ page }) => {
  await page.goto("/accounts?mock=success");
  await waitForMockWorker(page);

  const response = await page.evaluate(async () => {
    const result = await fetch("/api/accounts");
    return {
      body: (await result.json()) as ApiEnvelope<Array<{ id: string }>>,
      status: result.status,
    };
  });

  expect(response.status).toBe(200);
  expect(response.body.success).toBe(true);
  expect(response.body.data?.map(({ id }) => id)).toEqual(["acc_primary", "acc_pending"]);
  await expect(page.getByRole("table", { name: "账户列表" })).toBeVisible();
  await expect(page.getByText("主账号")).toBeVisible();
  await expect(page.getByText("待登录账号")).toBeVisible();
  await expect(page.getByText("2 个账户")).toBeVisible();
});

test("account detail keeps context while switching tabs and fits responsive baselines", async ({
  page,
}) => {
  await page.goto("/accounts?mock=success");
  await waitForMockWorker(page);

  await page.getByRole("link", { name: "打开账户 主账号" }).click();
  await expect(page).toHaveURL(/\/accounts\/acc_primary\/aliases$/);
  await expect(page.getByRole("heading", { level: 2, name: "主账号" })).toBeVisible();
  const detailNavigation = page.getByRole("navigation", { name: "主账号详情导航" });
  await expect(detailNavigation.getByRole("link", { name: "别名" })).toHaveAttribute(
    "aria-current",
    "page",
  );

  await detailNavigation.getByRole("link", { name: "收件箱" }).click();
  await expect(page).toHaveURL(/\/accounts\/acc_primary\/inbox$/);
  await expect(page.getByRole("heading", { level: 2, name: "主账号" })).toBeVisible();
  await expect(detailNavigation.getByRole("link", { name: "收件箱" })).toHaveAttribute(
    "aria-current",
    "page",
  );

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    const dimensions = await page.evaluate(() => ({
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
    }));
    expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
    await expect(page.getByRole("navigation", { name: "主账号详情导航" })).toBeVisible();
  }
});

test("inbox filters keep account context, preserve URL state, and fit responsive baselines", async ({
  page,
}) => {
  await page.goto("/accounts/acc_primary/inbox?mock=success&source=workspace");
  await waitForMockWorker(page, "收件箱");

  const accountSelect = page.getByLabel("账户");
  const aliasSelect = page.getByLabel("别名");
  const daysSelect = page.getByLabel("时间范围");
  const limitSelect = page.getByLabel("数量");
  await expect(accountSelect).toHaveValue("acc_primary");
  await expect(aliasSelect).toHaveValue("");
  await expect(daysSelect).toHaveValue("7");
  await expect(limitSelect).toHaveValue("20");
  const messageList = page.getByRole("list", { name: "邮件摘要列表" });
  await expect(messageList).toBeVisible();
  await expect(messageList).toContainText("登录确认");
  await expect(page.getByRole("region", { name: "登录确认" })).toBeVisible();

  await page.getByRole("button", { name: "选择邮件 新设备登录提醒" }).click();
  await expect(page.getByRole("region", { name: "新设备登录提醒" })).toContainText(
    "Apple <no_reply@email.apple.com>",
  );

  const filteredRequest = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return (
      url.pathname === "/api/inbox" && url.searchParams.get("alias") === "quiet-orchid@icloud.com"
    );
  });
  await aliasSelect.selectOption("quiet-orchid@icloud.com");
  await filteredRequest;
  await expect(page).toHaveURL(/mock=success.*source=workspace.*alias=quiet-orchid%40icloud.com/);
  await expect(aliasSelect).toHaveValue("quiet-orchid@icloud.com");

  const rangeRequest = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return (
      url.pathname === "/api/inbox" &&
      url.searchParams.get("days") === "3" &&
      url.searchParams.get("limit") === "50"
    );
  });
  await daysSelect.selectOption("3");
  await limitSelect.selectOption("50");
  await rangeRequest;
  await expect(page).toHaveURL(/days=3.*limit=50/);

  const refreshRequest = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return (
      url.pathname === "/api/inbox" &&
      url.searchParams.get("account_id") === "acc_primary" &&
      url.searchParams.get("alias") === "quiet-orchid@icloud.com" &&
      url.searchParams.get("days") === "3" &&
      url.searchParams.get("limit") === "50"
    );
  });
  await page.getByRole("button", { name: "刷新收件箱" }).click();
  await refreshRequest;
  await expect(page).toHaveURL(/alias=quiet-orchid%40icloud.com.*days=3.*limit=50/);

  await accountSelect.selectOption("acc_pending");
  await expect(page).toHaveURL(
    /\/accounts\/acc_pending\/inbox\?mock=success&source=workspace&days=3&limit=50$/,
  );
  await expect(accountSelect).toHaveValue("acc_pending");
  await expect(aliasSelect).toHaveValue("");
  await expect(daysSelect).toHaveValue("3");
  await expect(limitSelect).toHaveValue("50");

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    const dimensions = await page.evaluate(() => ({
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
    }));
    expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
    await expect(page.getByLabel("账户")).toBeVisible();
    await expect(page.getByLabel("别名")).toBeVisible();
    await expect(page.getByLabel("时间范围")).toBeVisible();
    await expect(page.getByLabel("数量")).toBeVisible();
  }
});

test("inbox displays the Web API fallback reported by the service", async ({ page }) => {
  await page.goto("/accounts/acc_primary/inbox?mock=web-api");
  await waitForMockWorker(page, "收件箱");

  await expect(page.getByLabel("实际读取方式：Web API")).toBeVisible();
  await expect(page.getByLabel("实际读取方式：IMAP")).toHaveCount(0);
  await expect(page.getByRole("list", { name: "邮件摘要列表" })).toBeVisible();
});

test("inbox long content fits each responsive layout without horizontal overflow", async ({
  page,
}) => {
  await page.goto("/accounts/acc_primary/inbox?mock=inbox-long");
  await waitForMockWorker(page, "收件箱");

  const messageItem = page.locator(".inbox-message-item");
  const previewPanel = page.locator(".inbox-preview-panel");
  const previewCopy = previewPanel.locator(".inbox-preview-copy p");
  await expect(messageItem).toHaveCount(1);
  await expect(previewPanel).toBeVisible();
  await expect(messageItem).toContainText("LongSender-");
  await expect(messageItem).toContainText("LongSubject-");
  await expect(messageItem).toContainText("long-recipient-");
  await expect(previewPanel).toContainText("LongSender-");
  await expect(previewPanel).toContainText("LongSubject-");
  await expect(previewPanel).toContainText("long-recipient-");
  await expect(previewCopy).toContainText("Preview line 18");

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    const dimensions = await page.evaluate(() => {
      const message = document.querySelector<HTMLElement>(".inbox-message-item");
      const preview = document.querySelector<HTMLElement>(".inbox-preview-panel");
      const previewCopy = document.querySelector<HTMLElement>(".inbox-preview-copy p");
      return {
        documentWidth: document.documentElement.scrollWidth,
        messageScrollWidth: message?.scrollWidth ?? 0,
        messageWidth: message?.clientWidth ?? 0,
        previewCopyScrollHeight: previewCopy?.scrollHeight ?? 0,
        previewCopyHeight: previewCopy?.clientHeight ?? 0,
        previewScrollWidth: preview?.scrollWidth ?? 0,
        previewWidth: preview?.clientWidth ?? 0,
        viewportWidth: window.innerWidth,
      };
    });

    expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
    expect(dimensions.messageScrollWidth).toBeLessThanOrEqual(dimensions.messageWidth);
    expect(dimensions.previewScrollWidth).toBeLessThanOrEqual(dimensions.previewWidth);
    expect(dimensions.previewCopyScrollHeight).toBeGreaterThan(dimensions.previewCopyHeight);
    await expect(messageItem).toBeVisible();
    await expect(previewPanel).toBeVisible();
  }
});

test("inbox keeps its filters visible for Apple fallback errors and gateway timeouts", async ({
  page,
}) => {
  await page.goto(
    "/accounts/acc_primary/inbox?mock=inbox-error&alias=quiet-orchid%40icloud.com&days=3&limit=50",
  );
  await waitForMockWorker(page, "收件箱");

  const fallbackAlert = page.getByRole("alert");
  await expect(fallbackAlert).toContainText("Apple 服务错误");
  await expect(page.getByLabel("别名")).toHaveValue("quiet-orchid@icloud.com");
  await expect(page.getByLabel("时间范围")).toHaveValue("3");
  await expect(page.getByLabel("数量")).toHaveValue("50");
  await expect(page.getByRole("button", { name: "重新加载" })).toBeVisible();
  await expect(page.getByRole("list", { name: "邮件摘要列表" })).toHaveCount(0);

  await page.goto("/accounts/acc_primary/inbox?mock=inbox-timeout");
  await waitForMockWorker(page, "收件箱");
  const timeoutAlert = page.getByRole("alert");
  await expect(timeoutAlert).toContainText("读取邮件超时");
  await expect(timeoutAlert).toContainText("读取邮件超时，请稍后重试。");
  await expect(page.getByRole("button", { name: "重新加载" })).toBeVisible();
});

test("alias list searches and filters without leaving the account context", async ({ page }) => {
  await page.goto("/accounts/acc_primary/aliases?mock=success");
  await waitForMockWorker(page, "别名");

  const table = page.getByRole("table", { name: "别名列表" });
  const searchInput = page.getByRole("searchbox", { name: "搜索别名" });
  const statusFilter = page.getByRole("group", { name: "别名状态筛选" });
  await expect(table).toBeVisible();
  await expect(page.getByText("quiet-orchid@icloud.com")).toBeVisible();
  await expect(page.getByText("silver-field@icloud.com")).toBeVisible();
  await expect(page.getByText("2 个别名")).toBeVisible();

  await searchInput.fill("GitHub");
  await expect(page.getByText("quiet-orchid@icloud.com")).toBeVisible();
  await expect(page.getByText("silver-field@icloud.com")).toHaveCount(0);
  await expect(page.getByText("1 / 2 个别名")).toBeVisible();
  await expect(page).toHaveURL(/mock=success.*q=GitHub/);

  await page.getByRole("button", { name: "清除搜索" }).click();
  await statusFilter.getByRole("button", { name: "已停用" }).click();
  await expect(page.getByText("quiet-orchid@icloud.com")).toHaveCount(0);
  await expect(page.getByText("silver-field@icloud.com")).toBeVisible();
  await expect(page).toHaveURL(/mock=success.*status=inactive/);

  await searchInput.fill("GitHub");
  await expect(page.getByRole("heading", { level: 3, name: "没有匹配的别名" })).toBeVisible();
  await page.getByRole("button", { name: "清除筛选" }).click();
  await expect(page.getByText("quiet-orchid@icloud.com")).toBeVisible();
  await expect(page.getByText("silver-field@icloud.com")).toBeVisible();

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    const dimensions = await page.evaluate(() => ({
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
    }));
    expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
    await expect(searchInput).toBeVisible();
    await expect(statusFilter).toBeVisible();
    await expect(table).toBeVisible();
  }
});

test("alias session expiration recovers through credentials and returns to the alias source", async ({
  page,
}) => {
  await page.goto("/accounts/acc_primary/aliases?mock=expired&status=active");
  await waitForMockWorker(page, "别名");

  const alert = page.getByRole("alert");
  await expect(alert).toContainText("Cookie 会话已过期");
  await expect(alert).toContainText("会话已过期，请更新 Cookie。");
  await expect(page.getByRole("link", { name: "更新 Cookie" })).toBeVisible();
  await expect(page.getByRole("button", { name: "创建别名" })).toHaveCount(0);
  await expect(page.getByRole("table", { name: "别名列表" })).toHaveCount(0);

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    const dimensions = await page.evaluate(() => ({
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
    }));
    expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
    await expect(alert).toBeVisible();
  }

  await page.getByRole("link", { name: "更新 Cookie" }).click();
  await expect(page).toHaveURL(/\/accounts\/acc_primary\/security$/);
  await expect(page.getByRole("alert")).toContainText("更新 Cookie 或重新登录后将返回原页面");
  const cookieSection = page.getByRole("region", { name: "Cookie" });
  const cookieInput = cookieSection.getByLabel("Cookie 数据");
  await expect(cookieInput).toBeFocused();

  await cookieInput.fill("session=browser-alias-recovery-value");
  await cookieSection.getByRole("button", { name: "更新 Cookie" }).click();

  await expect(page).toHaveURL(/\/accounts\/acc_primary\/aliases\?mock=expired&status=active$/);
  await expect(page.getByRole("table", { name: "别名列表" })).toBeVisible();
  await expect(page.getByText("quiet-orchid@icloud.com")).toBeVisible();
  await expect(page.getByText("1 / 2 个别名")).toBeVisible();
  await expect(page.getByRole("status")).toContainText("Cookie 已更新");
  await expect(page.locator("body")).not.toContainText("browser-alias-recovery-value");
  expect(page.url()).not.toContain("browser-alias-recovery-value");
});

test("forbidden alias sessions expose the same credential recovery entry", async ({ page }) => {
  await page.goto("/accounts/acc_primary/aliases?mock=alias-forbidden");
  await waitForMockWorker(page, "别名");

  const alert = page.getByRole("alert");
  await expect(alert).toContainText("Cookie 会话已过期");
  await expect(alert).toContainText("会话已过期，请更新 Cookie。");
  const recoveryLink = alert.getByRole("link", { name: "更新 Cookie" });
  await expect(recoveryLink).toHaveAttribute("href", "/accounts/acc_primary/security");
  await recoveryLink.click();
  await expect(page).toHaveURL(/\/accounts\/acc_primary\/security$/);
  await expect(page.getByLabel("Cookie 数据")).toBeFocused();
});

test("alias Apple service errors keep retryable context and recover once the service returns success", async ({
  page,
}) => {
  await page.goto("/accounts/acc_primary/aliases?mock=alias-error");
  await waitForMockWorker(page, "别名");

  const alert = page.getByRole("alert");
  await expect(alert).toContainText("Apple 服务错误");
  await expect(alert).toContainText("模拟 Apple 服务错误");
  await expect(page.getByRole("link", { name: "更新 Cookie" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "创建别名" })).toHaveCount(0);
  await expect(page.getByRole("table", { name: "别名列表" })).toHaveCount(0);
  await expect(page.getByRole("heading", { level: 2, name: "主账号" })).toBeVisible();
  await expect(page.getByRole("button", { name: "重新加载" })).toBeVisible();

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    const dimensions = await page.evaluate(() => ({
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
    }));
    expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
    await expect(alert).toBeVisible();
  }

  await page.getByRole("button", { name: "重新加载" }).click();
  await expect(alert).toContainText("Apple 服务错误");

  await page.goto("/accounts/acc_primary/aliases?mock=success");
  await waitForMockWorker(page, "别名");

  await expect(page.getByRole("table", { name: "别名列表" })).toBeVisible();
  await expect(page.getByText("quiet-orchid@icloud.com")).toBeVisible();
  await expect(page.getByText("silver-field@icloud.com")).toBeVisible();
  await expect(page.getByText("2 个别名")).toBeVisible();
  await expect(page.getByRole("alert")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "重新加载" })).toHaveCount(0);
});

test("alias email copy writes the clipboard and keeps row actions stable", async ({ page }) => {
  await page.context().grantPermissions(["clipboard-read", "clipboard-write"], {
    origin: "http://127.0.0.1:4174",
  });
  await page.goto("/accounts/acc_primary/aliases?mock=success");
  await waitForMockWorker(page, "别名");

  const email = "quiet-orchid@icloud.com";
  const row = page.getByRole("row").filter({ hasText: email });
  const copyButton = row.getByRole("button", { name: `复制邮箱 ${email}`, exact: true });
  await expect(copyButton).toBeVisible();
  const beforeCopy = await copyButton.boundingBox();
  expect(beforeCopy).not.toBeNull();

  await copyButton.click();

  const copiedButton = row.getByRole("button", { name: `已复制邮箱 ${email}`, exact: true });
  await expect(copiedButton).toBeVisible();
  await expect(page.getByRole("status")).toContainText("邮箱已复制");
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(email);
  const afterCopy = await copiedButton.boundingBox();
  expect(afterCopy).toEqual(beforeCopy);
  await expect(page).toHaveURL(/\/accounts\/acc_primary\/aliases\?mock=success$/);

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    const dimensions = await page.evaluate(() => ({
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
    }));
    expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
    await expect(row.locator(".alias-copy-button")).toBeVisible();
  }
});

test("alias status actions refresh the server state without leaving the account context", async ({
  page,
}) => {
  await page.goto("/accounts/acc_primary/aliases?mock=success");
  await waitForMockWorker(page, "别名");

  const email = "quiet-orchid@icloud.com";
  const row = page.getByRole("row").filter({ hasText: email });
  const statusButton = row.getByRole("button", { name: `停用别名 ${email}`, exact: true });
  await expect(statusButton).toBeVisible();
  const beforeAction = await statusButton.boundingBox();
  expect(beforeAction).not.toBeNull();

  await statusButton.click();

  const restoreButton = row.getByRole("button", { name: `恢复别名 ${email}`, exact: true });
  await expect(restoreButton).toBeVisible();
  await expect(row).toContainText("已停用");
  await expect(page.getByRole("status").filter({ hasText: "别名已停用" })).toBeVisible();
  expect(await restoreButton.boundingBox()).toEqual(beforeAction);
  await expect(page).toHaveURL(/\/accounts\/acc_primary\/aliases\?mock=success$/);

  await restoreButton.click();
  await expect(row.getByRole("button", { name: `停用别名 ${email}`, exact: true })).toBeVisible();
  await expect(row).toContainText("使用中");
  await expect(page.getByRole("status").filter({ hasText: "别名已恢复" })).toBeVisible();

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    const dimensions = await page.evaluate(() => ({
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
    }));
    expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
    await expect(row.getByRole("button", { name: `停用别名 ${email}`, exact: true })).toBeVisible();
  }
});

test("alias deletion requires confirmation and refreshes the mock server list", async ({
  page,
}) => {
  await page.goto("/accounts/acc_primary/aliases?mock=success");
  await waitForMockWorker(page, "别名");

  const email = "quiet-orchid@icloud.com";
  const row = page.getByRole("row").filter({ hasText: email });
  const deleteButton = row.getByRole("button", { name: `删除别名 ${email}`, exact: true });
  await expect(deleteButton).toBeVisible();
  const beforeCancel = await deleteButton.boundingBox();
  expect(beforeCancel).not.toBeNull();

  await deleteButton.click();
  const dialog = page.getByRole("alertdialog", { name: "确认删除别名？" });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText(`删除“${email}”后`);
  await expect(dialog).toContainText("无法恢复");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();

  await expect(dialog).toHaveCount(0);
  await expect(row).toBeVisible();
  expect(await deleteButton.boundingBox()).toEqual(beforeCancel);
  await expect(page).toHaveURL(/\/accounts\/acc_primary\/aliases\?mock=success$/);

  await deleteButton.click();
  const confirmDialog = page.getByRole("alertdialog", { name: "确认删除别名？" });
  await confirmDialog.getByRole("button", { name: "删除别名", exact: true }).click();

  await expect(confirmDialog).toHaveCount(0);
  await expect(row).toHaveCount(0);
  await expect(page.getByText("1 个别名")).toBeVisible();
  await expect(page.getByText("silver-field@icloud.com")).toBeVisible();
  await expect(page.getByRole("status")).toContainText("别名已删除");
  await expect(page.getByRole("status")).toContainText(email);
  await expect(page).toHaveURL(/\/accounts\/acc_primary\/aliases\?mock=success$/);

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    const dimensions = await page.evaluate(() => ({
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
    }));
    expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
    await expect(
      page.getByRole("button", { name: "删除别名 silver-field@icloud.com" }),
    ).toBeVisible();
  }
});

test("alias delete confirmation closes with Escape and returns focus to its trigger", async ({
  page,
}) => {
  await page.goto("/accounts/acc_primary/aliases?mock=success");
  await waitForMockWorker(page, "别名");

  const email = "quiet-orchid@icloud.com";
  const deleteButton = page.getByRole("button", {
    exact: true,
    name: `删除别名 ${email}`,
  });
  await deleteButton.focus();
  await expect(deleteButton).toBeFocused();

  await page.keyboard.press("Enter");
  const dialog = page.getByRole("alertdialog", { name: "确认删除别名？" });
  await expect(dialog).toBeVisible();

  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(deleteButton).toBeFocused();
});

test("alias creation refreshes the list once and remains usable across responsive baselines", async ({
  page,
}) => {
  await page.goto("/accounts/acc_primary/aliases?mock=success&q=missing&status=inactive");
  await waitForMockWorker(page, "别名");

  await expect(page.getByRole("heading", { level: 3, name: "没有匹配的别名" })).toBeVisible();
  await page.getByRole("button", { name: "创建别名" }).click();
  const dialog = page.getByRole("dialog", { name: "创建别名" });
  await expect(dialog).toBeVisible();

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    const dialogBox = await dialog.boundingBox();
    expect(dialogBox).not.toBeNull();
    expect(dialogBox!.x).toBeGreaterThanOrEqual(0);
    expect(dialogBox!.y).toBeGreaterThanOrEqual(0);
    expect(dialogBox!.x + dialogBox!.width).toBeLessThanOrEqual(viewport.width);
    expect(dialogBox!.y + dialogBox!.height).toBeLessThanOrEqual(viewport.height);
    const documentWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(documentWidth).toBeLessThanOrEqual(viewport.width);
  }

  await dialog.getByLabel("标签（可选）").fill("新闻订阅");
  await dialog.getByRole("button", { name: "创建别名", exact: true }).click();

  await expect(dialog).toHaveCount(0);
  const createdRow = page.getByRole("row").filter({ hasText: "new-alias@icloud.com" });
  await expect(createdRow).toBeVisible();
  await expect(createdRow).toContainText("新闻订阅");
  await expect(createdRow).toContainText("使用中");
  await expect(page.getByText("3 个别名")).toBeVisible();
  await expect(page.getByRole("status")).toContainText("别名已创建");
  await expect(page).toHaveURL(/\/accounts\/acc_primary\/aliases\?mock=success$/);

  const dimensions = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: window.innerWidth,
  }));
  expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
});

test("Cookie browser exports update the account without remaining in the page", async ({
  page,
}) => {
  await page.goto("/accounts?mock=success");
  await waitForMockWorker(page);

  await page.getByRole("link", { name: "设置凭据" }).click();
  await expect(page).toHaveURL(/\/accounts\/acc_pending\/security$/);
  const cookieSection = page.getByRole("region", { name: "Cookie" });
  const input = cookieSection.getByLabel("Cookie 数据");
  const cookieInput = JSON.stringify([
    { domain: ".icloud.com", name: "session", path: "/", value: "browser-token" },
    { name: "user", value: "browser-owner" },
  ]);

  await expect(cookieSection.getByText("未配置")).toBeVisible();
  await expect(input).toHaveClass(/secret-input-concealed/);
  await input.fill(cookieInput);
  await cookieSection.getByRole("button", { name: "更新 Cookie" }).click();

  await expect(input).toHaveValue("");
  await expect(cookieSection.getByText("已配置")).toBeVisible();
  await expect(page.getByRole("status")).toContainText("Cookie 已更新");
  await expect(page.getByText("正常")).toBeVisible();
  await expect(page.locator("body")).not.toContainText("browser-token");
  expect(page.url()).not.toContain("browser-token");

  await page.setViewportSize({ width: 390, height: 844 });
  const dimensions = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: window.innerWidth,
  }));
  expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
  await expect(cookieSection.getByRole("button", { name: "更新 Cookie" })).toBeVisible();
});

test("App Password validation updates capability without leaving the password", async ({
  page,
}) => {
  await page.goto("/accounts?mock=success");
  await waitForMockWorker(page);

  await page.getByRole("link", { name: "设置凭据" }).click();
  await expect(page).toHaveURL(/\/accounts\/acc_pending\/security$/);
  const passwordSection = page.getByRole("region", { name: "App 专用密码" });
  const emailInput = passwordSection.getByLabel("iCloud 邮箱");
  const passwordInput = passwordSection.getByLabel("App 专用密码", { exact: true });

  await expect(passwordSection.getByText("未配置")).toBeVisible();
  await expect(emailInput).toHaveValue("pending@icloud.com.cn");
  await expect(passwordInput).toHaveAttribute("type", "password");
  await passwordSection.getByRole("button", { name: "验证并保存" }).click();
  await expect(passwordSection.getByText("请输入 App 专用密码")).toBeVisible();

  await passwordInput.fill("abcd-efgh-ijkl-mnop");
  await passwordSection.getByRole("button", { name: "验证并保存" }).click();

  await expect(passwordInput).toHaveValue("");
  await expect(passwordSection.getByText("已配置")).toBeVisible();
  await expect(page.getByRole("status")).toContainText("App 密码已验证");
  await expect(page.locator("body")).not.toContainText("abcd-efgh-ijkl-mnop");
  expect(page.url()).not.toContain("abcd-efgh-ijkl-mnop");

  await page.setViewportSize({ width: 390, height: 844 });
  const dimensions = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: window.innerWidth,
  }));
  expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
  await expect(passwordSection.getByRole("button", { name: "验证并保存" })).toBeVisible();
});

test("Apple password login creates a Cookie session without leaving the password", async ({
  page,
}) => {
  await page.goto("/accounts?mock=success");
  await waitForMockWorker(page);

  await page.getByRole("link", { name: "设置凭据" }).click();
  await expect(page).toHaveURL(/\/accounts\/acc_pending\/security$/);
  const loginSection = page.getByRole("region", { name: "Apple 登录" });
  const passwordInput = loginSection.getByLabel("Apple ID 密码", { exact: true });

  await expect(loginSection.getByText("Cookie 未配置")).toBeVisible();
  await expect(passwordInput).toHaveAttribute("type", "password");
  await loginSection.getByRole("button", { name: "登录", exact: true }).click();
  await expect(loginSection.getByText("请输入 Apple ID 密码")).toBeVisible();

  await passwordInput.fill("browser-login-secret");
  await loginSection.getByRole("button", { name: "登录", exact: true }).click();

  await expect(passwordInput).toHaveValue("");
  await expect(loginSection.getByText("Cookie 已配置")).toBeVisible();
  await expect(page.getByRole("status")).toContainText("Apple 登录成功");
  await expect(page.getByText("正常")).toBeVisible();
  await expect(page.locator("body")).not.toContainText("browser-login-secret");
  expect(page.url()).not.toContain("browser-login-secret");

  await page.setViewportSize({ width: 390, height: 844 });
  const dimensions = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: window.innerWidth,
  }));
  expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
  await expect(loginSection.getByRole("button", { name: "登录", exact: true })).toBeVisible();
});

test("Apple OTP login consumes the challenge and refreshes the pending account", async ({
  page,
}) => {
  await page.goto("/accounts/acc_pending/security?mock=otp");
  await waitForMockWorker(page, "凭据");

  const loginSection = page.getByRole("region", { name: "Apple 登录" });
  const passwordInput = page.locator("#apple-login-password");
  await expect(loginSection.getByText("Cookie 未配置")).toBeVisible();
  await passwordInput.fill("browser-otp-secret");
  await loginSection.getByRole("button", { name: "登录", exact: true }).click();

  const dialog = page.getByRole("dialog", { name: "验证 Apple 登录" });
  const otpInput = dialog.getByLabel("6 位验证码");
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText(/双重认证 · [0-5]:\d{2} 后过期/);
  await expect(otpInput).toBeFocused();
  await expect(passwordInput).toHaveValue("");

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    const dialogBox = await dialog.boundingBox();
    expect(dialogBox).not.toBeNull();
    expect(dialogBox!.x).toBeGreaterThanOrEqual(0);
    expect(dialogBox!.y).toBeGreaterThanOrEqual(0);
    expect(dialogBox!.x + dialogBox!.width).toBeLessThanOrEqual(viewport.width);
    expect(dialogBox!.y + dialogBox!.height).toBeLessThanOrEqual(viewport.height);
    const documentWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(documentWidth).toBeLessThanOrEqual(viewport.width);
    await expect(dialog).toBeVisible();
  }

  await dialog.getByRole("button", { name: "验证", exact: true }).click();
  await expect(dialog.getByText("请输入 6 位数字验证码")).toBeVisible();
  await otpInput.fill("123456");
  await dialog.getByRole("button", { name: "验证", exact: true }).click();

  await expect(dialog).toHaveCount(0);
  await expect(loginSection.getByText("Cookie 已配置")).toBeVisible();
  await expect(page.getByRole("status")).toContainText("Apple 登录成功");
  await expect(page.getByText("正常")).toBeVisible();
  await expect(page.locator("body")).not.toContainText("browser-otp-secret");
  await expect(page.locator("body")).not.toContainText("123456");
  expect(page.url()).not.toContain("browser-otp-secret");
  expect(page.url()).not.toContain("123456");
  expect(page.url()).not.toContain("mock-login-challenge");

  await page.setViewportSize({ width: 390, height: 844 });
  const dimensions = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: window.innerWidth,
  }));
  expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
  await expect(loginSection.getByRole("button", { name: "登录", exact: true })).toBeVisible();
});

test("OTP dialog focuses its input and Escape restores the Apple login control", async ({
  page,
}) => {
  await page.goto("/accounts/acc_pending/security?mock=otp");
  await waitForMockWorker(page, "凭据");

  const loginSection = page.getByRole("region", { name: "Apple 登录" });
  const passwordInput = page.locator("#apple-login-password");
  const loginButton = loginSection.getByRole("button", { name: "登录", exact: true });
  await passwordInput.fill("keyboard-otp-secret");
  await loginButton.focus();
  await page.keyboard.press("Enter");

  const dialog = page.getByRole("dialog", { name: "验证 Apple 登录" });
  const otpInput = dialog.getByLabel("6 位验证码");
  await expect(dialog).toBeVisible();
  await expect(otpInput).toBeFocused();

  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(loginSection.getByRole("status")).toContainText("验证码验证已取消");
  await expect(loginButton).toBeEnabled();
  await expect(loginButton).toBeFocused();
});

test("credential workflows leave no browser-persistent residue or console disclosure", async ({
  page,
}) => {
  const consoleMessages: string[] = [];
  page.on("console", (message) => consoleMessages.push(message.text()));
  const secrets = {
    applePassword: "fe206-browser-apple-password",
    appPassword: "fe20-6app-secr-et00",
    challengeId: "mock-login-challenge",
    cookie: "fe206-browser-cookie-value",
    otp: "123456",
    proxyPassword: "fe206-browser-proxy-password",
  };

  await page.goto("/accounts/acc_pending/security?mock=success");
  await waitForMockWorker(page, "凭据");

  const cookieSection = page.getByRole("region", { name: "Cookie" });
  const cookieInput = cookieSection.getByLabel("Cookie 数据");
  await cookieInput.fill(`session=${secrets.cookie}`);
  await cookieSection.getByRole("button", { name: "更新 Cookie" }).click();
  await expect(cookieInput).toHaveValue("");
  await expect(cookieSection.getByText("已配置")).toBeVisible();

  const appPasswordSection = page.getByRole("region", { name: "App 专用密码" });
  const appPasswordInput = appPasswordSection.getByLabel("App 专用密码", { exact: true });
  await appPasswordInput.fill(secrets.appPassword);
  await appPasswordSection.getByRole("button", { name: "验证并保存" }).click();
  await expect(appPasswordInput).toHaveValue("");
  await expect(appPasswordSection.getByText("已配置")).toBeVisible();

  const appleLoginSection = page.getByRole("region", { name: "Apple 登录" });
  const applePasswordInput = appleLoginSection.getByLabel("Apple ID 密码", { exact: true });
  await applePasswordInput.fill(secrets.applePassword);
  await appleLoginSection.getByRole("button", { name: "登录", exact: true }).click();
  await expect(applePasswordInput).toHaveValue("");
  await expect(appleLoginSection.getByText("Cookie 已配置")).toBeVisible();

  await page.goto("/accounts/acc_pending/security?mock=otp");
  await waitForMockWorker(page, "凭据");
  const otpLoginSection = page.getByRole("region", { name: "Apple 登录" });
  await otpLoginSection
    .getByLabel("Apple ID 密码", { exact: true })
    .fill("fe206-browser-otp-password");
  await otpLoginSection.getByRole("button", { name: "登录", exact: true }).click();
  const otpDialog = page.getByRole("dialog", { name: "验证 Apple 登录" });
  await otpDialog.getByLabel("6 位验证码").fill(secrets.otp);
  await otpDialog.getByRole("button", { name: "验证", exact: true }).click();
  await expect(otpDialog).toHaveCount(0);

  await page.goto("/accounts?mock=empty");
  await waitForMockWorker(page);
  await page.getByRole("button", { name: "添加账户" }).click();
  const accountDialog = page.getByRole("dialog", { name: "添加账户" });
  await accountDialog.getByLabel("账户名称").fill("安全审计账户");
  await accountDialog.getByLabel("iCloud 邮箱").fill("audit@icloud.com");
  await accountDialog
    .getByLabel("代理（可选）")
    .fill(`http://proxy-user:${secrets.proxyPassword}@127.0.0.1:7890`);
  await accountDialog.getByRole("button", { name: "添加账户", exact: true }).click();
  await expect(accountDialog).toHaveCount(0);
  await expect(page.getByRole("link", { name: "打开账户 安全审计账户" })).toBeVisible();

  const snapshot = await browserPersistenceSnapshot(page);
  expect(snapshot.localStorage).toEqual({});
  expect(snapshot.sessionStorage).toEqual({});
  expect(snapshot.cacheEntries).toEqual([]);
  expect(snapshot.indexedDatabases).toEqual([]);

  const observableState = JSON.stringify({ consoleMessages, snapshot });
  for (const secret of [...Object.values(secrets), "fe206-browser-otp-password"]) {
    expect(observableState).not.toContain(secret);
  }
});

test("browser worker serves empty fixtures", async ({ page }) => {
  await page.goto("/accounts?mock=empty");
  await waitForMockWorker(page);

  const response = await page.evaluate(async () => {
    const result = await fetch("/api/accounts");
    return (await result.json()) as ApiEnvelope<unknown[]>;
  });

  expect(response).toMatchObject({ data: [], success: true });
  await expect(page.getByRole("heading", { level: 3, name: "暂无账户" })).toBeVisible();
  await expect(page.getByText("0 个账户")).toBeVisible();
});

test("browser worker serves error fixtures", async ({ page }) => {
  await page.goto("/accounts?mock=error");
  await waitForMockWorker(page);

  const response = await page.evaluate(async () => {
    const result = await fetch("/api/accounts");
    return {
      body: (await result.json()) as ApiEnvelope<unknown>,
      status: result.status,
    };
  });

  expect(response.status).toBe(502);
  expect(response.body).toMatchObject({ message: "模拟 Apple 服务错误", success: false });
  await expect(page.getByRole("alert")).toContainText("Apple 服务错误");
  await expect(page.getByRole("button", { name: "重新加载" })).toBeVisible();
});

test("browser worker exposes the offline state and retry action", async ({ page }) => {
  await page.goto("/accounts?mock=offline");
  await waitForMockWorker(page);

  await expect(page.getByRole("alert")).toContainText("无法连接到本地服务");
  await expect(page.getByRole("button", { name: "重新加载" })).toBeVisible();
  await expect(page.getByText("主账号")).toHaveCount(0);
});

test("settings reports health and reloads configuration", async ({ page }) => {
  await page.goto("/settings?mock=success");
  await waitForMockWorker(page, "系统设置");

  await expect(page.getByText("icloud-hme")).toBeVisible();
  await expect(page.getByText("正常")).toBeVisible();
  await expect(page.getByText("可用")).toBeVisible();
  await expect(page.getByText("服务端本地配置")).toBeVisible();
  const healthList = page.locator(".settings-health-list");
  const configLocation = healthList.locator(".settings-health-item").last();

  await page.getByRole("button", { name: "重载配置" }).click();
  await expect(page.getByRole("status")).toContainText("配置已重新加载");

  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    const dimensions = await page.evaluate(() => ({
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
    }));
    expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
    const widths = await Promise.all([
      healthList.evaluate((element) => element.getBoundingClientRect().width),
      configLocation.evaluate((element) => element.getBoundingClientRect().width),
    ]);
    expect(widths[1]).toBeGreaterThanOrEqual(widths[0] - 2);
    await expect(page.getByRole("button", { name: "重载配置" })).toBeVisible();
  }
});

test("settings keeps health and reload errors recoverable", async ({ page }) => {
  await page.goto("/settings?mock=error");
  await waitForMockWorker(page, "系统设置");

  await expect(page.getByRole("alert")).toContainText("健康检查失败");
  await expect(page.getByRole("button", { name: "重新检查" })).toBeVisible();
  await page.getByRole("button", { name: "重载配置" }).click();
  await expect(page.getByRole("alert")).toContainText("Apple 服务错误");
  await expect(page.getByRole("button", { name: "重载配置" })).toBeEnabled();
});

test("expired account recovery updates Cookie and returns to the source", async ({ page }) => {
  await page.goto("/accounts?mock=mixed");
  await waitForMockWorker(page);

  const accountRow = page.getByRole("row").filter({ hasText: "需要恢复的账号" });
  await expect(accountRow).toBeVisible();
  await expect(accountRow.getByText("异常")).toBeVisible();
  await expect(accountRow.getByText("Cookie 已配置")).toBeVisible();
  await expect(accountRow.getByText("App 密码 未配置")).toBeVisible();
  const recoveryLink = accountRow.getByRole("link", { name: "更新 Cookie" });
  await expect(recoveryLink).toHaveAttribute("href", "/accounts/acc_error/security");

  await page.setViewportSize({ width: 390, height: 844 });
  const dimensions = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: window.innerWidth,
  }));
  expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);

  await recoveryLink.click();
  await expect(page).toHaveURL(/\/accounts\/acc_error\/security$/);
  const recoveryAlert = page.getByRole("alert");
  await expect(recoveryAlert).toContainText("Cookie 会话已过期");
  await expect(recoveryAlert).toContainText("更新 Cookie 或重新登录后将返回原页面");
  const cookieInput = page.getByLabel("Cookie 数据");
  await expect(cookieInput).toBeFocused();

  await cookieInput.fill("session=browser-recovery-value");
  await page
    .getByRole("region", { name: "Cookie" })
    .getByRole("button", {
      name: "更新 Cookie",
    })
    .click();

  await expect(page).toHaveURL(/\/accounts\?mock=mixed$/);
  await expect(page.getByRole("status")).toContainText("Cookie 已更新");
  const recoveredRow = page.getByRole("row").filter({ hasText: "需要恢复的账号" });
  await expect(recoveredRow.getByText("正常")).toBeVisible();
  await expect(recoveredRow.getByRole("link", { name: "更新 Cookie" })).toHaveCount(0);
  await expect(page.locator("body")).not.toContainText("browser-recovery-value");
  expect(page.url()).not.toContain("browser-recovery-value");
});

test("account creation validates, refreshes the list, and announces success", async ({ page }) => {
  await page.goto("/accounts?mock=empty");
  await waitForMockWorker(page);

  await page.getByRole("button", { name: "添加账户" }).click();
  const dialog = page.getByRole("dialog", { name: "添加账户" });
  await dialog.getByRole("button", { name: "添加账户", exact: true }).click();
  await expect(dialog.getByText("请输入账户名称")).toBeVisible();
  await expect(dialog.getByText("请输入 iCloud 邮箱")).toBeVisible();

  await dialog.getByLabel("账户名称").fill("浏览器新账户");
  await dialog.getByLabel("iCloud 邮箱").fill("browser@icloud.com");
  await dialog.getByLabel("区域").selectOption("icloud.com.cn");
  await dialog.getByLabel("代理（可选）").fill("http://127.0.0.1:7890");
  await dialog.getByRole("button", { name: "添加账户", exact: true }).click();

  const createdRow = page.getByRole("row").filter({ hasText: "浏览器新账户" });
  await expect(createdRow).toBeVisible();
  await expect(createdRow.getByText("browser@icloud.com")).toBeVisible();
  await expect(page.getByRole("status")).toContainText("已添加账户“浏览器新账户”");
  await expect(page.getByRole("dialog", { name: "添加账户" })).toHaveCount(0);
});

test("account creation keeps entered values on server error", async ({ page }) => {
  await page.goto("/accounts?mock=error");
  await waitForMockWorker(page);

  await page.getByRole("button", { name: "添加账户" }).click();
  const dialog = page.getByRole("dialog", { name: "添加账户" });
  await dialog.getByLabel("账户名称").fill("错误后保留");
  await dialog.getByLabel("iCloud 邮箱").fill("keep@icloud.com");
  await dialog.getByRole("button", { name: "添加账户", exact: true }).click();

  await expect(dialog.getByRole("alert")).toContainText("Apple 服务错误");
  await expect(dialog.getByLabel("账户名称")).toHaveValue("错误后保留");
  await expect(dialog.getByLabel("iCloud 邮箱")).toHaveValue("keep@icloud.com");
});

test("account deletion confirms, refreshes the list, and announces success", async ({ page }) => {
  await page.goto("/accounts?mock=success");
  await waitForMockWorker(page);

  await page.getByRole("button", { name: "删除账户 主账号" }).click();
  const dialog = page.getByRole("alertdialog", { name: "确认删除账户？" });
  await expect(dialog).toContainText("删除“主账号”后");
  await dialog.getByRole("button", { name: "删除账户", exact: true }).click();

  await expect(page.getByRole("button", { name: "删除账户 主账号" })).toHaveCount(0);
  await expect(page.getByText("1 个账户")).toBeVisible();
  await expect(page.getByRole("status")).toContainText("账户已删除");
  await expect(page.getByRole("status")).toContainText("主账号");
  await expect(dialog).toHaveCount(0);
});
