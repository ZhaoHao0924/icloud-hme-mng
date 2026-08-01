import { describe, expect, it } from "vitest";

import { ApiError, createApiClient } from "./client";
import {
  accountsQueryOptions,
  aliasesQueryOptions,
  healthQueryOptions,
  inboxQueryOptions,
  queryKeys,
} from "./queries";
import { createQueryClient } from "../app/queryClient";
import { accountFixtures, aliasFixtures } from "../test/fixtures";
import { mockScenario } from "../test/mocks/server";

const testApi = createApiClient({ baseUrl: "http://localhost/api" });

describe("query integration", () => {
  it("uses stable query keys with normalized inbox defaults", () => {
    expect(queryKeys.accounts).toEqual(["accounts"]);
    expect(queryKeys.account("acc_primary")).toEqual(["account", "acc_primary"]);
    expect(queryKeys.aliases("acc_primary")).toEqual(["aliases", "acc_primary"]);
    expect(queryKeys.inbox({ accountId: "acc_primary" })).toEqual([
      "inbox",
      "acc_primary",
      "",
      20,
      7,
    ]);
  });

  it("loads validated success fixtures through QueryClient and MSW", async () => {
    const client = createQueryClient();

    const accounts = await client.fetchQuery(accountsQueryOptions(testApi));
    const aliases = await client.fetchQuery(aliasesQueryOptions("acc_primary", testApi));
    const health = await client.fetchQuery(healthQueryOptions(testApi));

    expect(accounts).toEqual(accountFixtures);
    expect(aliases).toEqual({
      account_id: "acc_primary",
      aliases: aliasFixtures,
      count: aliasFixtures.length,
    });
    expect(health).toMatchObject({ status: "ok", config_available: true });
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
