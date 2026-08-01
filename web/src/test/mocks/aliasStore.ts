import type { Alias, CreatedAlias } from "../../api/schemas";

export type MockCreateAliasInput = {
  accountId: string;
  label: string;
};

export function createMockAliasStore() {
  let aliasesByAccount = new Map<string, Alias[]>();
  let activeOverrides = new Map<string, Map<string, boolean>>();
  let deletedByAccount = new Map<string, Set<string>>();
  let nextId = 1;

  function aliasesForAccount(accountId: string, fixtures: Alias[]) {
    const overrides = activeOverrides.get(accountId);
    const deleted = deletedByAccount.get(accountId);
    return [...(aliasesByAccount.get(accountId) ?? []), ...fixtures]
      .filter((alias) => !deleted?.has(alias.anonymousId))
      .map((alias) => ({
        ...alias,
        active: overrides?.get(alias.anonymousId) ?? alias.active,
      }));
  }

  return {
    create({ accountId, label }: MockCreateAliasInput): CreatedAlias {
      const id = nextId++;
      const email = id === 1 ? "new-alias@icloud.com" : `new-alias-${id}@icloud.com`;
      const createdAt = "2026-08-01T09:00:00Z";
      const alias: Alias = {
        active: true,
        anonymousId: `alias_created_${id}`,
        createdAt,
        email,
        label,
      };
      aliasesByAccount.set(accountId, [alias, ...(aliasesByAccount.get(accountId) ?? [])]);
      return {
        account_id: accountId,
        created_at: createdAt,
        email,
        label,
      };
    },
    delete(accountId: string, aliasId: string, fixtures: Alias[] = []) {
      const alias = aliasesForAccount(accountId, fixtures).find(
        (candidate) => candidate.anonymousId === aliasId,
      );
      if (!alias) return false;

      aliasesByAccount.set(
        accountId,
        (aliasesByAccount.get(accountId) ?? []).filter(
          (candidate) => candidate.anonymousId !== aliasId,
        ),
      );
      const deleted = deletedByAccount.get(accountId) ?? new Set<string>();
      deleted.add(aliasId);
      deletedByAccount.set(accountId, deleted);

      const overrides = activeOverrides.get(accountId);
      overrides?.delete(aliasId);
      if (overrides?.size === 0) {
        activeOverrides.delete(accountId);
      }

      return true;
    },
    list(accountId: string, fixtures: Alias[] = []) {
      return aliasesForAccount(accountId, fixtures);
    },
    setActive(accountId: string, aliasId: string, active: boolean, fixtures: Alias[] = []) {
      const alias = aliasesForAccount(accountId, fixtures).find(
        (candidate) => candidate.anonymousId === aliasId,
      );
      if (!alias) return false;
      const overrides = activeOverrides.get(accountId) ?? new Map<string, boolean>();
      overrides.set(aliasId, active);
      activeOverrides.set(accountId, overrides);
      return true;
    },
    reset() {
      aliasesByAccount = new Map<string, Alias[]>();
      activeOverrides = new Map<string, Map<string, boolean>>();
      deletedByAccount = new Map<string, Set<string>>();
      nextId = 1;
    },
  };
}

export type MockAliasStore = ReturnType<typeof createMockAliasStore>;
