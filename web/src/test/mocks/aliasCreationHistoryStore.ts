import type { AliasCreationHistory, AliasCreationHistoryEntry } from "../../api/schemas";

export type MockAliasCreationHistoryInput = Omit<
  AliasCreationHistoryEntry,
  "batch_id" | "created_at"
>;

export function createMockAliasCreationHistoryStore() {
  let entriesByAccount = new Map<string, AliasCreationHistoryEntry[]>();
  let nextBatchID = 1;

  return {
    get(accountId: string): AliasCreationHistory {
      const entries = entriesByAccount.get(accountId) ?? [];
      return {
        account_id: accountId,
        count: entries.length,
        entries: entries.map((entry) => ({
          ...entry,
          aliases: entry.aliases.map((alias) => ({ ...alias })),
        })),
      };
    },
    record(accountId: string, input: MockAliasCreationHistoryInput) {
      const entry: AliasCreationHistoryEntry = {
        ...input,
        aliases: input.aliases.map((alias) => ({ ...alias })),
        batch_id: `batch_mock_${nextBatchID++}`,
        created_at: "2026-08-02T09:00:00.000Z",
      };
      const entries = [entry, ...(entriesByAccount.get(accountId) ?? [])].slice(0, 500);
      entriesByAccount.set(accountId, entries);
      return { ...entry, aliases: entry.aliases.map((alias) => ({ ...alias })) };
    },
    reset() {
      entriesByAccount = new Map<string, AliasCreationHistoryEntry[]>();
      nextBatchID = 1;
    },
  };
}

export type MockAliasCreationHistoryStore = ReturnType<typeof createMockAliasCreationHistoryStore>;
