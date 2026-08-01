import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  forbidOnly: Boolean(process.env.CI),
  fullyParallel: false,
  reporter: "list",
  retries: process.env.CI ? 2 : 0,
  testDir: "./e2e-mock",
  timeout: 30_000,
  use: {
    baseURL: "http://127.0.0.1:4174",
    trace: "on-first-retry",
  },
  webServer: {
    command: "npm run dev:mock -- --host 127.0.0.1 --port 4174 --strictPort",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    url: "http://127.0.0.1:4174/accounts?mock=success",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
