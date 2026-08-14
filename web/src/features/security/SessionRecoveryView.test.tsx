import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { ApiError } from "../../api/client";
import { AccountRequestErrorState } from "./SessionRecoveryView";
import { readSessionRecoveryLocationState } from "./sessionRecoveryState";

describe("SessionExpiredState", () => {
  it("routes a confirmed session-expiry code to credentials with its source", async () => {
    const user = userEvent.setup();
    const router = createMemoryRouter(
      [
        {
          path: "/accounts/:accountId/aliases",
          element: (
            <AccountRequestErrorState
              accountId="acc_main"
              error={
                new ApiError({
                  code: "icloud_session_expired",
                  kind: "http",
                  message: "upstream rejected",
                  status: 401,
                })
              }
            />
          ),
        },
        { path: "/accounts/:accountId/security", element: <div>凭据恢复目标</div> },
      ],
      { initialEntries: ["/accounts/acc_main/aliases?status=active"] },
    );
    render(<RouterProvider router={router} />);

    expect(screen.getByRole("alert")).toHaveTextContent("Cookie 会话已过期");
    expect(screen.getByRole("alert")).toHaveTextContent("会话已过期，请更新 Cookie。");
    const recoveryLink = screen.getByRole("link", { name: "更新 Cookie" });
    expect(recoveryLink).toHaveAttribute("href", "/accounts/acc_main/security");

    await user.click(recoveryLink);

    expect(screen.getByText("凭据恢复目标")).toBeInTheDocument();
    expect(readSessionRecoveryLocationState(router.state.location.state)).toEqual({
      from: "/accounts/acc_main/aliases?status=active",
      reason: "icloud_session_expired",
    });
  });

  it("does not route a bare HTTP 403 to Cookie recovery", () => {
    const router = createMemoryRouter(
      [
        {
          path: "/accounts/:accountId/aliases",
          element: (
            <AccountRequestErrorState
              accountId="acc_main"
              error={new ApiError({ kind: "http", message: "upstream rejected", status: 403 })}
            />
          ),
        },
      ],
      { initialEntries: ["/accounts/acc_main/aliases"] },
    );
    render(<RouterProvider router={router} />);

    expect(screen.queryByRole("link", { name: "更新 Cookie" })).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).not.toHaveTextContent("Cookie 会话已过期");
  });

  it("keeps non-session errors retryable without a credential link", async () => {
    const user = userEvent.setup();
    const onRetry = vi.fn();
    const router = createMemoryRouter(
      [
        {
          path: "/accounts/:accountId/inbox",
          element: (
            <AccountRequestErrorState
              accountId="acc_main"
              error={
                new ApiError({
                  kind: "http",
                  code: "icloud_service_unavailable",
                  message: "读取邮件失败",
                  retryable: true,
                  status: 502,
                })
              }
              onRetry={onRetry}
            />
          ),
        },
      ],
      { initialEntries: ["/accounts/acc_main/inbox"] },
    );
    render(<RouterProvider router={router} />);

    expect(screen.getByRole("alert")).toHaveTextContent("Apple 服务暂时不可用，请稍后重试。");
    expect(screen.queryByRole("link", { name: "更新 Cookie" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "重新加载" }));
    expect(onRetry).toHaveBeenCalledOnce();
  });
});
