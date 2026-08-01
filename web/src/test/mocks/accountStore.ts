import type { Account } from "../../api/schemas";

export type MockCreateAccountInput = {
  host?: string;
  icloud_email?: string;
  name?: string;
  proxy?: string;
};

export function createMockAccountStore() {
  let createdAccounts: Account[] = [];
  let deletedAccountIds = new Set<string>();
  let loginChallenges = new Map<string, string>();
  let nextId = 1;
  let restoredSessionAccountIds = new Set<string>();
  let updatedAccounts = new Map<string, Account>();

  function list(fixtures: Account[] = []) {
    return [...fixtures, ...createdAccounts]
      .filter((account) => !deletedAccountIds.has(account.id))
      .map((account) => ({ ...(updatedAccounts.get(account.id) ?? account) }));
  }

  function invalidateLoginChallenge(accountId: string) {
    for (const [challengeId, ownerAccountId] of loginChallenges) {
      if (ownerAccountId === accountId) loginChallenges.delete(challengeId);
    }
  }

  return {
    create(input: MockCreateAccountInput) {
      const account: Account = {
        alias_active: 0,
        alias_total: 0,
        created_at: "2026-08-01T10:00:00+08:00",
        has_app_password: false,
        has_cookies: false,
        host: input.host || "icloud.com",
        icloud_email: input.icloud_email || "",
        id: `acc_created_${nextId++}`,
        last_error: "",
        last_validated: "",
        name: input.name?.trim() || "未命名账户",
        proxy_configured: Boolean(input.proxy?.trim()),
        real_email: "",
        status: "pending",
      };
      createdAccounts = [...createdAccounts, account];
      return account;
    },
    delete(accountId: string) {
      createdAccounts = createdAccounts.filter((account) => account.id !== accountId);
      deletedAccountIds.add(accountId);
      invalidateLoginChallenge(accountId);
      restoredSessionAccountIds.delete(accountId);
      updatedAccounts.delete(accountId);
      return { id: accountId };
    },
    consumeLoginChallenge(accountId: string, challengeId: string) {
      if (loginChallenges.get(challengeId) !== accountId) return false;
      loginChallenges.delete(challengeId);
      return true;
    },
    get(accountId: string, fixtures: Account[] = []) {
      return list(fixtures).find((account) => account.id === accountId);
    },
    invalidateLoginChallenge,
    issueLoginChallenge(accountId: string, challengeId: string) {
      invalidateLoginChallenge(accountId);
      loginChallenges.delete(challengeId);
      loginChallenges.set(challengeId, accountId);
    },
    hasRestoredSession(accountId: string) {
      return restoredSessionAccountIds.has(accountId);
    },
    list,
    markSessionRestored(accountId: string) {
      restoredSessionAccountIds.add(accountId);
    },
    reset() {
      createdAccounts = [];
      deletedAccountIds = new Set<string>();
      loginChallenges = new Map<string, string>();
      nextId = 1;
      restoredSessionAccountIds = new Set<string>();
      updatedAccounts = new Map<string, Account>();
    },
    update(account: Account) {
      const created = createdAccounts.some((candidate) => candidate.id === account.id);
      if (created) {
        createdAccounts = createdAccounts.map((candidate) =>
          candidate.id === account.id ? { ...account } : candidate,
        );
      } else {
        updatedAccounts.set(account.id, { ...account });
      }
      return { ...account };
    },
  };
}

export type MockAccountStore = ReturnType<typeof createMockAccountStore>;
