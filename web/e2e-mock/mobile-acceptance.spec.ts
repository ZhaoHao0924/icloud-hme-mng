import { expect, test } from "@playwright/test";

const qa010Viewports = [
  { height: 667, name: "phone-compact", width: 375 },
  { height: 844, name: "phone-standard", width: 390 },
  { height: 1024, name: "tablet-portrait", width: 768 },
  { height: 900, name: "desktop", width: 1440 },
] as const;

const viewportGeometryTolerance = 2;

async function waitForMockWorker(page: import("@playwright/test").Page, pageTitle: string) {
  await expect(page.getByRole("heading", { level: 1, name: pageTitle })).toBeVisible();
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);
}

async function expectNoHorizontalOverflow(page: import("@playwright/test").Page) {
  const dimensions = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: window.visualViewport?.width ?? window.innerWidth,
  }));

  expect(dimensions.bodyWidth).toBeLessThanOrEqual(Math.ceil(dimensions.viewportWidth));
  expect(dimensions.documentWidth).toBeLessThanOrEqual(Math.ceil(dimensions.viewportWidth));
}

async function expectReachable(
  page: import("@playwright/test").Page,
  target: import("@playwright/test").Locator,
) {
  await target.scrollIntoViewIfNeeded();
  await expect(target).toBeVisible();

  const [box, viewport] = await Promise.all([
    target.boundingBox(),
    page.evaluate(() => ({
      height: window.visualViewport?.height ?? window.innerHeight,
      width: window.visualViewport?.width ?? window.innerWidth,
    })),
  ]);

  if (!box) throw new Error("Expected a visible control to have a layout box");

  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.y).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(
    Math.ceil(viewport.width) + viewportGeometryTolerance,
  );
  expect(box.y + box.height).toBeLessThanOrEqual(
    Math.ceil(viewport.height) + viewportGeometryTolerance,
  );
}

for (const viewport of qa010Viewports) {
  test(`QA-010 core workflows remain usable at ${viewport.name}`, async ({ page }) => {
    await page.setViewportSize(viewport);

    await page.goto("/accounts?mock=success");
    await waitForMockWorker(page, "账户");
    await expectNoHorizontalOverflow(page);

    const primaryNavigation = page.getByRole("navigation", { name: "主导航" });
    if (viewport.width <= 760) {
      const menuButton = page.getByRole("button", { name: "展开主菜单" });
      await expect(menuButton).toBeVisible();
      await expect(primaryNavigation).toBeHidden();
      await menuButton.click();
      await expect(primaryNavigation).toBeVisible();
      await page.getByRole("button", { name: "收起主菜单" }).click();
      await expect(primaryNavigation).toBeHidden();
    } else {
      await expect(page.getByRole("button", { name: "展开主菜单" })).toBeHidden();
      await expect(primaryNavigation).toBeVisible();
    }

    const addAccount = page.getByRole("button", { name: "添加账户", exact: true });
    await expectReachable(page, addAccount);
    await addAccount.focus();
    await page.keyboard.press("Enter");

    const addAccountDialog = page.getByRole("dialog", { name: "添加账户" });
    await expectReachable(page, addAccountDialog);
    await expectReachable(
      page,
      addAccountDialog.getByRole("button", { name: "添加账户", exact: true }),
    );
    await page.keyboard.press("Escape");
    await expect(addAccountDialog).toHaveCount(0);
    await expect(addAccount).toBeFocused();

    await page.goto("/accounts/acc_primary/aliases?mock=success");
    await waitForMockWorker(page, "别名");
    await expectNoHorizontalOverflow(page);

    const createAlias = page.getByRole("button", { name: "创建别名", exact: true }).first();
    await expectReachable(page, createAlias);
    await createAlias.focus();
    await page.keyboard.press("Enter");

    const createAliasDialog = page.getByRole("dialog", { name: "创建别名" });
    await expectReachable(page, createAliasDialog);
    await expectReachable(
      page,
      createAliasDialog.getByRole("button", { name: "创建别名", exact: true }),
    );
    await page.keyboard.press("Escape");
    await expect(createAliasDialog).toHaveCount(0);
    await expect(createAlias).toBeFocused();

    await page.goto("/accounts/acc_primary/security?mock=success");
    await waitForMockWorker(page, "凭据");
    await expectNoHorizontalOverflow(page);
    await expectReachable(page, page.getByLabel("Cookie 数据"));
    await expectReachable(page, page.getByRole("button", { name: "更新 Cookie", exact: true }));
    await expectReachable(page, page.getByRole("button", { name: "验证并保存", exact: true }));

    await page.goto("/accounts/acc_primary/inbox?mock=success");
    await waitForMockWorker(page, "收件箱");
    await expectNoHorizontalOverflow(page);
    await expectReachable(page, page.getByLabel("账户"));
    await expectReachable(page, page.getByLabel("别名"));
    await expectReachable(page, page.getByLabel("时间范围"));
    await expectReachable(page, page.getByLabel("数量"));

    await page.goto("/settings?mock=success");
    await waitForMockWorker(page, "设置");
    await expectNoHorizontalOverflow(page);
    await expectReachable(page, page.getByRole("button", { name: "刷新日志" }));
    await expect(page.getByRole("list", { name: "最近操作记录" })).toBeVisible();
  });
}
