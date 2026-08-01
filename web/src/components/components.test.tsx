import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Database } from "lucide-react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";

import { ConfirmDialog } from "./ConfirmDialog";
import { EmptyState } from "./EmptyState";
import { ErrorState } from "./ErrorState";
import { LoadingState } from "./LoadingState";
import { NotificationProvider } from "./NotificationProvider";
import { useNotifications } from "./notificationContext";

function NotificationHarness() {
  const { notify } = useNotifications();
  return (
    <button
      type="button"
      onClick={() =>
        notify({
          duration: 0,
          message: "账户已重新加载",
          title: "操作完成",
          tone: "success",
        })
      }
    >
      显示通知
    </button>
  );
}

function ConfirmDialogHarness({
  onConfirm,
  onConfirmError,
}: {
  onConfirm: () => void | Promise<void>;
  onConfirmError?: (error: unknown) => void;
}) {
  const [open, setOpen] = useState(true);
  return (
    <ConfirmDialog
      confirmLabel="删除"
      description="删除后无法恢复。"
      destructive
      onConfirm={onConfirm}
      onConfirmError={onConfirmError}
      onOpenChange={setOpen}
      open={open}
      title="删除主账号？"
    />
  );
}

describe("shared state components", () => {
  it("renders accessible loading, empty, and error states", () => {
    const { rerender } = render(<LoadingState label="正在读取账户" />);
    expect(screen.getByRole("status")).toHaveTextContent("正在读取账户");

    rerender(
      <EmptyState
        action={<button type="button">添加账户</button>}
        description="添加一个账户以开始管理别名。"
        icon={<Database size={20} />}
        title="暂无账户"
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("暂无账户");
    expect(screen.getByRole("button", { name: "添加账户" })).toBeEnabled();

    rerender(<ErrorState description="无法连接到服务" />);
    expect(screen.getByRole("alert")).toHaveTextContent("无法连接到服务");
  });

  it("announces and dismisses notifications", async () => {
    const user = userEvent.setup();
    render(
      <NotificationProvider>
        <NotificationHarness />
      </NotificationProvider>,
    );

    await user.click(screen.getByRole("button", { name: "显示通知" }));

    expect(screen.getByRole("status")).toHaveTextContent("操作完成");
    expect(screen.getByRole("status")).toHaveTextContent("账户已重新加载");

    await user.click(screen.getByRole("button", { name: "关闭通知" }));
    expect(screen.queryByText("操作完成")).not.toBeInTheDocument();
  });

  it("keeps an async confirmation open while pending and closes after success", async () => {
    const user = userEvent.setup();
    let resolveConfirmation: (() => void) | undefined;
    const onConfirm = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveConfirmation = resolve;
        }),
    );

    render(<ConfirmDialogHarness onConfirm={onConfirm} />);
    const confirmButton = screen.getByRole("button", { name: "删除" });

    await user.click(confirmButton);
    expect(onConfirm).toHaveBeenCalledOnce();
    expect(screen.getByRole("button", { name: "处理中" })).toBeDisabled();
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();

    resolveConfirmation?.();
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
  });

  it("keeps the confirmation open when the operation fails", async () => {
    const user = userEvent.setup();
    const failure = new Error("persist failed");
    const onConfirmError = vi.fn();

    render(
      <ConfirmDialogHarness
        onConfirm={() => Promise.reject(failure)}
        onConfirmError={onConfirmError}
      />,
    );

    await user.click(screen.getByRole("button", { name: "删除" }));

    expect(onConfirmError).toHaveBeenCalledWith(failure);
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "删除" })).toBeEnabled();
  });
});
