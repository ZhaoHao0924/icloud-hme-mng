import { HttpResponse, http } from "msw";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { AppProviders } from "../../app/AppProviders";
import { createQueryClient } from "../../app/queryClient";
import { routes } from "../../app/router";
import { server } from "../../test/mocks/server";

function renderSettings() {
  const router = createMemoryRouter(routes, { initialEntries: ["/settings"] });
  const queryClient = createQueryClient();
  const view = render(
    <AppProviders queryClient={queryClient}>
      <RouterProvider router={router} />
    </AppProviders>,
  );
  return { ...view, queryClient, router };
}

describe("SettingsPage", () => {
  it("renders the service health and configuration availability", async () => {
    renderSettings();

    expect(await screen.findByText("icloud-hme")).toBeInTheDocument();
    expect(screen.getByText("正常")).toBeInTheDocument();
    expect(screen.getByText("dev")).toBeInTheDocument();
    expect(screen.getByText("可用")).toBeInTheDocument();
    expect(screen.getByText("服务端本地配置")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重载配置" })).toBeInTheDocument();
  });

  it("reloads configuration, refreshes health, and announces success", async () => {
    const user = userEvent.setup();
    let healthRequests = 0;
    server.use(
      http.get("*/api/health", () => {
        healthRequests += 1;
        return HttpResponse.json({
          data: {
            config_available: true,
            service: "icloud-hme",
            status: "ok",
            version: "test-version",
          },
          success: true,
        });
      }),
    );
    renderSettings();

    await screen.findByText("test-version");
    const reloadButton = screen.getByRole("button", { name: "重载配置" });
    await user.click(reloadButton);

    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("配置已重新加载"));
    expect(healthRequests).toBeGreaterThanOrEqual(2);
    expect(reloadButton).toBeEnabled();
  });

  it("keeps a retryable health error when the service check fails", async () => {
    server.use(
      http.get("*/api/health", () =>
        HttpResponse.json({ message: "health unavailable", success: false }, { status: 502 }),
      ),
    );
    renderSettings();

    expect(await screen.findByRole("alert")).toHaveTextContent("健康检查失败");
    expect(screen.getByRole("button", { name: "重新检查" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重载配置" })).toBeEnabled();
  });

  it("surfaces a configuration reload error without losing the action", async () => {
    const user = userEvent.setup();
    server.use(
      http.post("*/api/reload", () =>
        HttpResponse.json({ message: "reload unavailable", success: false }, { status: 502 }),
      ),
    );
    renderSettings();

    await screen.findByText("icloud-hme");
    await user.click(screen.getByRole("button", { name: "重载配置" }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("reload unavailable"));
    expect(screen.getByRole("button", { name: "重载配置" })).toBeEnabled();
  });
});
