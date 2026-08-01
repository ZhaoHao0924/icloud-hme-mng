import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const apiProxyTarget = env.VITE_API_PROXY_TARGET || "http://127.0.0.1:8081";

  return {
    plugins: [react()],
    build: {
      rollupOptions: {
        output: {
          manualChunks: {
            "form-vendor": ["@hookform/resolvers", "react-hook-form", "zod"],
            "query-vendor": ["@tanstack/react-query"],
            "react-vendor": ["react", "react-dom", "react-router-dom"],
            "ui-vendor": ["@radix-ui/react-alert-dialog", "@radix-ui/react-dialog", "lucide-react"],
          },
        },
      },
    },
    server:
      mode === "mock"
        ? undefined
        : {
            proxy: {
              "/api": {
                changeOrigin: false,
                target: apiProxyTarget,
              },
            },
          },
  };
});
