import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { NotificationProvider } from "../../components/NotificationProvider";
import { AliasCopyButton } from "./AliasCopyButton";

const originalClipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, "clipboard");

function mockClipboard(writeText: (text: string) => Promise<void>) {
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
}

afterEach(() => {
  if (originalClipboardDescriptor) {
    Object.defineProperty(navigator, "clipboard", originalClipboardDescriptor);
  } else {
    Reflect.deleteProperty(navigator, "clipboard");
  }
});

function renderCopyButton(email = "alias@icloud.com") {
  return render(
    <NotificationProvider>
      <AliasCopyButton email={email} />
    </NotificationProvider>,
  );
}

describe("AliasCopyButton", () => {
  it("locks while writing and exposes success feedback after copying", async () => {
    let resolveCopy: (() => void) | undefined;
    const writeText = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveCopy = resolve;
        }),
    );
    const user = userEvent.setup();
    mockClipboard(writeText);
    renderCopyButton();

    await user.click(screen.getByRole("button", { name: "复制邮箱 alias@icloud.com" }));
    expect(writeText).toHaveBeenCalledOnce();
    expect(writeText).toHaveBeenCalledWith("alias@icloud.com");
    expect(screen.getByRole("button", { name: "正在复制邮箱 alias@icloud.com" })).toBeDisabled();

    resolveCopy?.();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "已复制邮箱 alias@icloud.com" })).toBeEnabled(),
    );
    const notificationRegion = screen.getByLabelText("通知");
    expect(within(notificationRegion).getByRole("status")).toHaveTextContent("邮箱已复制");
    expect(within(notificationRegion).getByRole("status")).toHaveTextContent("alias@icloud.com");
  });

  it("restores the copy action and reports a denied clipboard write", async () => {
    const user = userEvent.setup();
    mockClipboard(() => Promise.reject(new DOMException("Denied", "NotAllowedError")));
    renderCopyButton();

    await user.click(screen.getByRole("button", { name: "复制邮箱 alias@icloud.com" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("复制失败");
    expect(screen.getByRole("alert")).toHaveTextContent("无法写入剪贴板");
    expect(screen.getByRole("button", { name: "复制邮箱 alias@icloud.com" })).toBeEnabled();
  });
});
