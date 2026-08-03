import { describe, expect, it } from "vitest";

import { ApiError, createApiClient } from "./client";
import {
  accountsQueryOptions,
  aliasAutomationQueryOptions,
  aliasesQueryOptions,
  healthQueryOptions,
  inboxQueryOptions,
  operationLogsQueryOptions,
  queryKeys,
} from "./queries";
import { createQueryClient } from "../app/queryClient";
import { accountFixtures, aliasFixtures, operationLogsFixture } from "../test/fixtures";
import { mockScenario } from "../test/mocks/server";

const testApi = createApiClient({ baseUrl: "http://localhost/api" });

describe("query integration", () => {
  it("uses stable query keys with normalized inbox defaults", () => {
    expect(queryKeys.accounts).toEqual(["accounts"]);
    expect(queryKeys.account("acc_primary")).toEqual(["account", "acc_primary"]);
    expect(queryKeys.aliasAutomation("acc_primary")).toEqual(["alias-automation", "acc_primary"]);
    expect(queryKeys.aliases("acc_primary")).toEqual(["aliases", "acc_primary"]);
    expect(queryKeys.operationLogs).toEqual(["operation-logs"]);
    expect(queryKeys.inbox({ accountId: "acc_primary" })).toEqual([
      "inbox",
      "acc_primary",
      "",
      20,
      7,
    ]);
    expect(queryKeys.inboxFeed({ accountId: "acc_primary" }, 2)).toEqual([
      "inbox",
      "acc_primary",
      "",
      20,
      7,
      "feed",
      2,
    ]);
    expect(queryKeys.inboxMessage("acc_primary", "1042")).toEqual([
      "inbox-message",
      "acc_primary",
      "1042",
    ]);
  });

  it("loads validated success fixtures through QueryClient and MSW", async () => {
    const client = createQueryClient();

    const accounts = await client.fetchQuery(accountsQueryOptions(testApi));
    const automation = await client.fetchQuery(aliasAutomationQueryOptions("acc_primary", testApi));
    const aliases = await client.fetchQuery(aliasesQueryOptions("acc_primary", testApi));
    const health = await client.fetchQuery(healthQueryOptions(testApi));
    const operationLogs = await client.fetchQuery(operationLogsQueryOptions(testApi));

    expect(accounts).toEqual(accountFixtures);
    expect(automation).toMatchObject({ enabled: false, interval_minutes: 60, max_batch_size: 5 });
    expect(aliases).toEqual({
      account_id: "acc_primary",
      aliases: aliasFixtures,
      count: aliasFixtures.length,
    });
    expect(health).toMatchObject({ status: "ok", config_available: true });
    expect(operationLogs).toEqual(operationLogsFixture);
  });

  it("switches the same handlers to empty data", async () => {
    mockScenario.set("empty");
    const client = createQueryClient();

    const accounts = await client.fetchQuery(accountsQueryOptions(testApi));
    const inbox = await client.fetchQuery(inboxQueryOptions({ accountId: "acc_primary" }, testApi));

    expect(accounts).toEqual([]);
    expect(inbox).toMatchObject({ count: 0, messages: [] });
  });

  it("surfaces the error scenario without retrying HTTP failures", async () => {
    mockScenario.set("error");
    const client = createQueryClient();

    await expect(client.fetchQuery(accountsQueryOptions(testApi))).rejects.toMatchObject({
      kind: "http",
      status: 502,
    } satisfies Partial<ApiError>);
  });

  it("disables retries for mutations by default", () => {
    const client = createQueryClient();

    expect(client.getDefaultOptions().mutations?.retry).toBe(false);
  });
});
