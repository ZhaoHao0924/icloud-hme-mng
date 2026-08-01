import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    clearMocks: true,
    css: true,
    environment: "jsdom",
    // The MSW interceptor and mock stores are shared by the test runtime.
    // Run files serially so per-test resets cannot race with another file's requests.
    fileParallelism: false,
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: "./src/test/setup.ts",
  },
});
