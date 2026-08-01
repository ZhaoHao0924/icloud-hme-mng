import { useQuery } from "@tanstack/react-query";
import { HttpResponse, http } from "msw";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { accountsQueryOptions } from "../api/queries";
import { accountFixtures, healthyServiceFixture } from "../test/fixtures";
import { server } from "../test/mocks/server";
import { AppProviders } from "./AppProviders";
import { createQueryClient } from "./queryClient";

const testApiToken = "fe306-memory-only-api-token";

function apiTokenFailure() {
  return HttpResponse.json(
    {
      code: "api_token_invalid",
      message: "API 访问令牌无效或缺失",
      success: false,
    },
    { status: 401 },
  );
}

function AccountProbe() {
  const accountsQuery = useQuery(accountsQueryOptions());
  if (accountsQuery.isSuccess) {
    return <p>{accountsQuery.data[0]?.name ?? "没有账户"}</p>;
  }
  if (accountsQuery.isError) {
    return <p>账户请求失败</p>;
  }
  return <p>正在加载账户</p>;
}

function renderProbe() {
  const queryClient = createQueryClient();
  render(
    <AppProviders queryClient={queryClient}>
      <AccountProbe />
    </AppProviders>,
  );
  return queryClient;
}

describe("ApiTokenProvider", () => {
  it("verifies an in-memory token, retries active queries, and leaves no token residue", async () => {
    const user = userEvent.setup();
    const accountHeaders: Array<string | null> = [];
    const healthHeaders: Array<string | null> = [];
    server.use(
      http.get("*/api/accounts", ({ request }) => {
        const authorization = request.headers.get("authorization");
        accountHeaders.push(authorization);
        if (authorization !== `Bearer ${testApiToken}`) {
          return apiTokenFailure();
        }
        return HttpResponse.json({ data: accountFixtures, success: true });
      }),
      http.get("*/api/health", ({ request }) => {
        const authorization = request.headers.get("authorization");
        healthHeaders.push(authorization);
        if (authorization !== `Bearer ${testApiToken}`) {
          return apiTokenFailure();
        }
        return HttpResponse.json({ data: healthyServiceFixture, success: true });
      }),
    );

    const queryClient = renderProbe();
    const dialog = await screen.findByRole("dialog", { name: "API 访问令牌" });
    const input = within(dialog).getByLabelText("API 访问令牌");

    expect(input).toHaveAttribute("type", "password");
    await user.type(input, testApiToken);
    await user.click(within(dialog).getByRole("button", { name: "验证并继续" }));

    await expect(screen.findByText(accountFixtures[0].name)).resolves.toBeInTheDocument();
    await waitFor(() => {
      expect(accountHeaders).toContain(`Bearer ${testApiToken}`);
      expect(healthHeaders).toEqual([`Bearer ${testApiToken}`]);
    });
    expect(input).toHaveValue("");

    const observableState = JSON.stringify({
      document: document.documentElement.outerHTML,
      localStorage: Object.fromEntries(
        Array.from({ length: window.localStorage.length }, (_, index) => {
          const key = window.localStorage.key(index) ?? "";
          return [key, window.localStorage.getItem(key)];
        }),
      ),
      mutationStates: queryClient
        .getMutationCache()
        .getAll()
        .map((mutation) => mutation.state),
      queryStates: queryClient
        .getQueryCache()
        .getAll()
        .map((query) => query.state),
      sessionStorage: Object.fromEntries(
        Array.from({ length: window.sessionStorage.length }, (_, index) => {
          const key = window.sessionStorage.key(index) ?? "";
          return [key, window.sessionStorage.getItem(key)];
        }),
      ),
      url: window.location.href,
    });
    expect(observableState).not.toContain(testApiToken);
  });

  it("clears rejected input without turning it into an iCloud Cookie recovery", async () => {
    const user = userEvent.setup();
    server.use(
      http.get("*/api/accounts", () => apiTokenFailure()),
      http.get("*/api/health", () => apiTokenFailure()),
    );

    renderProbe();
    const dialog = await screen.findByRole("dialog", { name: "API 访问令牌" });
    const input = within(dialog).getByLabelText("API 访问令牌");

    await user.type(input, "fe306-rejected-api-token");
    await user.click(within(dialog).getByRole("button", { name: "验证并继续" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent("令牌无效或已过期");
    expect(input).toHaveValue("");
    expect(screen.queryByRole("link", { name: "更新 Cookie" })).not.toBeInTheDocument();
  });
});
