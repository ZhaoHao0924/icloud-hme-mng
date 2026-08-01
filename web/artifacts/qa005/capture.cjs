const fs = require("node:fs");
const path = require("node:path");
const { spawn } = require("node:child_process");
const { chromium, expect } = require("@playwright/test");

const rootDir = path.resolve(__dirname, "..", "..");
const outDir = __dirname;
const port = 4177;
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
      env: { ...process.env, CI: process.env.CI ?? "1" },
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

async function waitForMockWorker(page) {
  await expect(page.locator("h1")).toBeVisible();
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);
}

async function captureViewport(browser, name, size) {
  const context = await browser.newContext({ viewport: size });
  const page = await context.newPage();
  try {
    await page.goto(`${baseUrl}/accounts/acc_primary/inbox?mock=inbox-long`);
    await waitForMockWorker(page);
    await expect(page.locator(".inbox-message-item")).toHaveCount(1);
    await expect(page.locator(".inbox-preview-panel")).toBeVisible();
    await expectNoHorizontalOverflow(page);
    await page.screenshot({ path: path.join(outDir, `${name}-inbox-long.png`), fullPage: true });

    await page.goto(`${baseUrl}/settings?mock=success`);
    await waitForMockWorker(page);
    await expect(page.locator(".settings-health-list")).toBeVisible();
    await expect(page.locator(".settings-reload-button")).toBeVisible();
    await expectNoHorizontalOverflow(page);
    await page.screenshot({ path: path.join(outDir, `${name}-settings.png`), fullPage: true });
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
    if (!server.killed) server.kill("SIGKILL");
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
