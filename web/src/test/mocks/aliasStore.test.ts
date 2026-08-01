import { describe, expect, it } from "vitest";

import { aliasFixtures } from "../fixtures";
import { createMockAliasStore } from "./aliasStore";

describe("mock alias store", () => {
  it("persists fixture and created alias deletions until reset", () => {
    const store = createMockAliasStore();

    expect(store.list("acc_primary", aliasFixtures)).toHaveLength(2);
    expect(store.delete("acc_primary", "alias_active_1", aliasFixtures)).toBe(true);
    expect(store.list("acc_primary", aliasFixtures).map((alias) => alias.anonymousId)).toEqual([
      "alias_inactive_1",
    ]);

    const created = store.create({ accountId: "acc_primary", label: "临时服务" });
    const createdAlias = store
      .list("acc_primary", aliasFixtures)
      .find((alias) => alias.email === created.email);
    expect(createdAlias).toBeDefined();
    expect(store.delete("acc_primary", createdAlias?.anonymousId ?? "", aliasFixtures)).toBe(true);
    expect(store.list("acc_primary", aliasFixtures).map((alias) => alias.email)).toEqual([
      "silver-field@icloud.com",
    ]);
    expect(store.delete("acc_primary", "missing_alias", aliasFixtures)).toBe(false);

    store.reset();
    expect(store.list("acc_primary", aliasFixtures)).toHaveLength(2);
  });
});
