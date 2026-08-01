const fs = require("node:fs");
const path = require("node:path");
const { spawn } = require("node:child_process");
const { chromium, expect } = require("@playwright/test");

const rootDir = path.resolve(__dirname, "..", "..");
const outDir = __dirname;
const port = 4176;
const baseUrl = `http://127.0.0.1:${port}`;
const viteBin = path.join(rootDir, "node_modules", "vite", "bin", "vite.js");

const viewports = [
  { name: "desktop", size: { width: 1440, height: 900 } },
  { name: "tablet", size: { width: 1024, height: 768 } },
  { name: "mobile", size: { width: 390, height: 844 } },
];

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForServer(url, timeoutMs = 60_000) {
  const deadline = Date.now() + timeoutMs;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url, { cache: "no-store" });
      if (response.ok) return;
      lastError = new Error(`Unexpected status ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await delay(500);
  }
  throw lastError ?? new Error(`Timed out waiting for ${url}`);
}

function startServer() {
  return spawn(
    process.execPath,
    [viteBin, "--mode", "mock", "--host", "127.0.0.1", "--port", String(port), "--strictPort"],
    {
      cwd: rootDir,
      env: {
        ...process.env,
        CI: process.env.CI ?? "1",
      },
      stdio: "inherit",
    },
  );
}

async function expectNoHorizontalOverflow(page) {
  const dimensions = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: window.innerWidth,
  }));
  expect(dimensions.documentWidth).toBeLessThanOrEqual(dimensions.viewportWidth);
}

async function captureViewport(browser, name, size) {
  const context = await browser.newContext({ viewport: size });
  const page = await context.newPage();
  try {
    await page.goto(`${baseUrl}/accounts/acc_primary/aliases?mock=expired&status=active`);
    await expect(page.getByRole("heading", { level: 3, name: "邮箱别名" })).toBeVisible();

    const alert = page.getByRole("alert");
    await expect(alert).toContainText("Cookie 会话已过期");
    await expect(alert).toContainText("会话已过期，请更新 Cookie。");
    await expect(page.getByRole("link", { name: "更新 Cookie" })).toBeVisible();
    await expect(page.getByRole("button", { name: "创建别名" })).toHaveCount(0);
    await expect(page.getByRole("table", { name: "别名列表" })).toHaveCount(0);
    await expectNoHorizontalOverflow(page);
    await page.screenshot({ path: path.join(outDir, `${name}-expired.png`) });

    await page.getByRole("link", { name: "更新 Cookie" }).click();
    await expect(page).toHaveURL(/\/accounts\/acc_primary\/security$/);
    await expect(page.getByRole("alert")).toContainText("更新 Cookie 或重新登录后将返回原页面");
    const cookieSection = page.getByRole("region", { name: "Cookie" });
    const cookieInput = cookieSection.getByLabel("Cookie 数据");
    await expect(cookieInput).toBeFocused();
    await cookieInput.fill("session=visual-alias-recovery-value");
    await cookieSection.getByRole("button", { name: "更新 Cookie" }).click();

    await expect(page).toHaveURL(/\/accounts\/acc_primary\/aliases\?mock=expired&status=active$/);
    await expect(page.getByRole("table", { name: "别名列表" })).toBeVisible();
    await expect(page.getByText("quiet-orchid@icloud.com")).toBeVisible();
    await expect(page.getByText("1 / 2 个别名")).toBeVisible();
    await expect(page.getByRole("status")).toContainText("Cookie 已更新");
    await expect(page.locator("body")).not.toContainText("visual-alias-recovery-value");
    await page.getByRole("button", { name: "关闭通知" }).click();
    await expect(page.getByRole("status")).toHaveCount(0);
    await expectNoHorizontalOverflow(page);
    await page.screenshot({ path: path.join(outDir, `${name}-recovered.png`) });
  } finally {
    await context.close();
  }
}

async function main() {
  fs.mkdirSync(outDir, { recursive: true });
  const server = startServer();

  try {
    await waitForServer(`${baseUrl}/accounts?mock=success`);

    const browser = await chromium.launch({ headless: true });
    try {
      for (const { name, size } of viewports) {
        await captureViewport(browser, name, size);
      }
    } finally {
      await browser.close();
    }
  } finally {
    server.kill("SIGTERM");
    await delay(1000);
    if (!server.killed) {
      server.kill("SIGKILL");
    }
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
