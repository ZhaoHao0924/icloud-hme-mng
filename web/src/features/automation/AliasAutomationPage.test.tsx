import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { AppProviders } from "../../app/AppProviders";
import { createQueryClient } from "../../app/queryClient";
import { routes } from "../../app/router";
import { mockAliasAutomation } from "../../test/mocks/server";

function renderAutomation(path = "/accounts/acc_primary/automation") {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  const view = render(
    <AppProviders queryClient={createQueryClient()}>
      <RouterProvider router={router} />
    </AppProviders>,
  );
  return { ...view, router };
}

describe("AliasAutomationPage", () => {
  it("renders the account rule with inventory status and editable defaults", async () => {
    renderAutomation();

    expect(await screen.findByRole("heading", { name: "别名自动化" })).toBeInTheDocument();
    expect(screen.getByText("1 个")).toBeInTheDocument();
    expect(screen.getByLabelText("启用自动化规则")).not.toBeChecked();
    expect(screen.getByLabelText("执行间隔（分钟）")).toHaveValue(60);
    expect(screen.getByLabelText("单次上限")).toHaveValue(5);
    expect(screen.getByRole("button", { name: "批量创建" })).toBeInTheDocument();
  });

  it("saves a scheduled rule and executes it on demand", async () => {
    mockAliasAutomation.update("acc_primary", {
      enabled: false,
      interval_minutes: 60,
      label_prefix: "自动补充",
      max_batch_size: 5,
      minimum_active: 0,
      scheduled_batch_size: 0,
      target_active: 0,
    });
    const user = userEvent.setup();
    renderAutomation();

    await screen.findByRole("heading", { name: "别名自动化" });
    await user.click(screen.getByLabelText("启用自动化规则"));
    await user.clear(screen.getByLabelText("定时创建数量"));
    await user.type(screen.getByLabelText("定时创建数量"), "2");
    await user.click(screen.getByRole("button", { name: "保存规则" }));

    expect(await screen.findByText("自动化规则已保存")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText("启用自动化规则")).toBeChecked());

    await user.click(screen.getByRole("button", { name: "立即执行规则" }));
    expect(await screen.findByText("自动化规则已执行")).toBeInTheDocument();
    expect(screen.getByText("已创建 2 个别名")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("3 个")).toBeInTheDocument());
  });

  it("creates multiple aliases from the batch dialog", async () => {
    const user = userEvent.setup();
    renderAutomation();

    await screen.findByRole("heading", { name: "别名自动化" });
    await user.click(screen.getByRole("button", { name: "批量创建" }));
    const dialog = await screen.findByRole("dialog", { name: "批量创建别名" });
    await user.clear(screen.getByLabelText("创建数量"));
    await user.type(screen.getByLabelText("创建数量"), "3");
    await user.click(screen.getByRole("button", { name: "创建别名" }));

    expect(await screen.findByText("批量创建已完成")).toBeInTheDocument();
    expect(screen.getByText("已创建 3 个别名")).toBeInTheDocument();
    expect(dialog).not.toBeInTheDocument();
  });
});
