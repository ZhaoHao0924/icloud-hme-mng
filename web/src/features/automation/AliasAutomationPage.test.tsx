import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { AppProviders } from "../../app/AppProviders";
import { createQueryClient } from "../../app/queryClient";
import { routes } from "../../app/router";
import { mockAliasAutomation, mockAliasCreationHistory } from "../../test/mocks/server";

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
    expect(screen.getByLabelText("总别名安全上限")).toHaveValue(1000);
    expect(screen.getByLabelText("连续失败上限")).toHaveValue(3);
    expect(screen.getByLabelText("每日自动创建上限")).toHaveValue(0);
    expect(screen.getByLabelText("累计创建目标")).toHaveValue(0);
    expect(screen.getByLabelText("周一")).not.toBeChecked();
    expect(screen.getByLabelText("开始")).toHaveValue("");
    expect(screen.getByLabelText("结束")).toHaveValue("");
    expect(screen.getByRole("button", { name: "批量创建" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "创建历史" })).toBeInTheDocument();
  });

  it("paginates creation history entries", async () => {
    for (let index = 0; index < 11; index += 1) {
      mockAliasCreationHistory.record("acc_primary", {
        aliases: [],
        complete: true,
        created: 0,
        error: "",
        failed: 0,
        label_prefix: "",
        requested: 0,
        status: "success",
        trigger: "batch",
      });
    }

    const user = userEvent.setup();
    renderAutomation();

    expect(await screen.findByText("batch_mock_11")).toBeInTheDocument();
    expect(screen.queryByText("batch_mock_1")).not.toBeInTheDocument();
    expect(screen.getByText("1 / 2 页")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "下一页" }));

    expect(await screen.findByText("batch_mock_1")).toBeInTheDocument();
    expect(screen.queryByText("batch_mock_11")).not.toBeInTheDocument();
    expect(screen.getByText("2 / 2 页")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
  });

  it("expands and collapses aliases in their own history row", async () => {
    mockAliasCreationHistory.record("acc_primary", {
      aliases: [
        {
          created_at: "2026-08-02T09:00:00.000Z",
          email: "first@example.test",
          label: "first",
        },
        {
          created_at: "2026-08-02T09:00:00.000Z",
          email: "second@example.test",
          label: "second",
        },
      ],
      complete: true,
      created: 2,
      error: "",
      failed: 0,
      label_prefix: "",
      requested: 2,
      status: "success",
      trigger: "batch",
    });

    const user = userEvent.setup();
    renderAutomation();

    const toggle = await screen.findByRole("button", { name: /2/ });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("first@example.test")).not.toBeInTheDocument();

    await user.click(toggle);

    expect(await screen.findByText("first@example.test")).toBeInTheDocument();
    expect(screen.getByText("second@example.test")).toBeInTheDocument();
    expect(toggle).toHaveAttribute("aria-expanded", "true");

    await user.click(toggle);

    await waitFor(() => {
      expect(screen.queryByText("first@example.test")).not.toBeInTheDocument();
    });
  });

  it("saves a schedule window and previews the current rule without creating aliases", async () => {
    const user = userEvent.setup();
    renderAutomation();

    await screen.findByRole("heading", { name: "别名自动化" });
    await user.click(screen.getByLabelText("启用自动化规则"));
    await user.clear(screen.getByLabelText("定时创建数量"));
    await user.type(screen.getByLabelText("定时创建数量"), "2");
    await user.click(screen.getByLabelText("周一"));
    fireEvent.change(screen.getByLabelText("开始"), { target: { value: "09:00" } });
    fireEvent.change(screen.getByLabelText("结束"), { target: { value: "17:00" } });
    await user.click(screen.getByRole("button", { name: "保存规则" }));

    expect(await screen.findByText("自动化规则已保存")).toBeInTheDocument();
    expect(screen.getByLabelText("周一")).toBeChecked();
    expect(screen.getByLabelText("开始")).toHaveValue("09:00");
    expect(screen.getByLabelText("结束")).toHaveValue("17:00");

    await user.click(screen.getByRole("button", { name: "预览执行" }));

    expect(await screen.findByRole("region", { name: "执行预览" })).toBeInTheDocument();
    expect(screen.getByText("本次可创建")).toBeInTheDocument();
    expect(screen.queryByText("自动化规则已执行")).not.toBeInTheDocument();
  });

  it("saves a scheduled rule and executes it on demand", async () => {
    mockAliasAutomation.update("acc_primary", {
      enabled: false,
      interval_minutes: 60,
      label_prefix: "自动补充",
      max_batch_size: 5,
      max_total_aliases: 1000,
      max_failure_count: 3,
      daily_creation_limit: 0,
      minimum_active: 0,
      scheduled_batch_size: 0,
      target_active: 0,
      target_created: 0,
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

  it("pauses and resumes an enabled rule without changing its progress", async () => {
    mockAliasAutomation.update("acc_primary", {
      enabled: true,
      interval_minutes: 60,
      label_prefix: "自动补充",
      max_batch_size: 5,
      max_total_aliases: 1000,
      max_failure_count: 3,
      daily_creation_limit: 0,
      minimum_active: 0,
      scheduled_batch_size: 2,
      target_active: 0,
      target_created: 0,
    });
    const user = userEvent.setup();
    renderAutomation();

    await screen.findByRole("heading", { name: "别名自动化" });
    await user.click(screen.getByRole("button", { name: "暂停规则" }));

    expect(await screen.findByText("自动化规则已暂停")).toBeInTheDocument();
    expect(screen.getAllByText("规则已手动暂停").length).toBeGreaterThan(0);
    expect(screen.getByLabelText("启用自动化规则")).not.toBeChecked();
    expect(screen.getByRole("button", { name: "恢复规则" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "恢复规则" }));

    expect(await screen.findByText("自动化规则已恢复")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText("启用自动化规则")).toBeChecked());
  });

  it("defers additional automation work after reaching the daily creation limit", async () => {
    mockAliasAutomation.update("acc_primary", {
      enabled: true,
      interval_minutes: 60,
      label_prefix: "每日限制",
      max_batch_size: 5,
      max_total_aliases: 1000,
      max_failure_count: 3,
      daily_creation_limit: 2,
      minimum_active: 0,
      scheduled_batch_size: 5,
      target_active: 0,
      target_created: 0,
    });
    const user = userEvent.setup();
    renderAutomation();

    await screen.findByRole("heading", { name: "别名自动化" });
    await user.click(screen.getByRole("button", { name: "立即执行规则" }));

    expect(await screen.findByText("已达到每日自动创建上限")).toBeInTheDocument();
    expect(screen.getByText("已创建 2 个别名，将在次日继续")).toBeInTheDocument();
    expect(screen.getAllByText("2 / 2").length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: "立即执行规则" })).not.toBeDisabled();
  });

  it("stops a cumulative rule after the target is reached", async () => {
    const user = userEvent.setup();
    renderAutomation();

    await screen.findByRole("heading", { name: "别名自动化" });
    await user.click(screen.getByLabelText("启用自动化规则"));
    await user.clear(screen.getByLabelText("累计创建目标"));
    await user.type(screen.getByLabelText("累计创建目标"), "5");
    await user.click(screen.getByRole("button", { name: "保存规则" }));

    const confirmation = await screen.findByRole("alertdialog", {
      name: "确认重置累计创建进度",
    });
    expect(confirmation).toHaveTextContent("从头计数");
    await user.click(within(confirmation).getByRole("button", { name: "确认重置" }));

    expect(await screen.findByText("自动化规则已保存")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "立即执行规则" }));

    expect(await screen.findByText("自动化规则已自动暂停")).toBeInTheDocument();
    expect(screen.getAllByText("5 / 5").length).toBeGreaterThan(0);
    expect(screen.getAllByText(/累计创建目标已完成/).length).toBeGreaterThan(0);
    expect(screen.getByLabelText("启用自动化规则")).not.toBeChecked();
    expect(screen.getByRole("button", { name: "立即执行规则" })).toBeDisabled();
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
    expect(await screen.findByText("batch_mock_1")).toBeInTheDocument();
    expect(dialog).not.toBeInTheDocument();
  });
});
