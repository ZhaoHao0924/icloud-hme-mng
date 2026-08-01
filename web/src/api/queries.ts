import { queryOptions } from "@tanstack/react-query";

import { api, type ApiClient, type InboxQuery } from "./client";

const defaultInboxLimit = 20;
const defaultInboxDays = 7;
const inboxRequestTimeoutMs = 15_000;

function normalizedInboxQuery(query: InboxQuery) {
  return {
    accountId: query.accountId,
    alias: query.alias ?? "",
    days: query.days ?? defaultInboxDays,
    limit: query.limit ?? defaultInboxLimit,
  };
}

export const queryKeys = {
  account: (accountId: string) => ["account", accountId] as const,
  accounts: ["accounts"] as const,
  aliases: (accountId: string) => ["aliases", accountId] as const,
  health: ["health"] as const,
  inbox: (query: InboxQuery) => {
    const normalized = normalizedInboxQuery(query);
    return [
      "inbox",
      normalized.accountId,
      normalized.alias,
      normalized.limit,
      normalized.days,
    ] as const;
  },
};

export function accountsQueryOptions(client: ApiClient = api) {
  return queryOptions({
    queryFn: ({ signal }) => client.listAccounts({ signal }),
    queryKey: queryKeys.accounts,
  });
}

export function aliasesQueryOptions(accountId: string, client: ApiClient = api) {
  return queryOptions({
    enabled: accountId !== "",
    queryFn: ({ signal }) => client.listAliases(accountId, { signal }),
    queryKey: queryKeys.aliases(accountId),
  });
}

export function healthQueryOptions(client: ApiClient = api) {
  return queryOptions({
    queryFn: ({ signal }) => client.getHealth({ signal }),
    queryKey: queryKeys.health,
  });
}

export function inboxQueryOptions(query: InboxQuery, client: ApiClient = api) {
  const normalized = normalizedInboxQuery(query);
  return queryOptions({
    enabled: normalized.accountId !== "",
    queryFn: ({ signal }) =>
      client.listInbox(normalized, { signal, timeoutMs: inboxRequestTimeoutMs }),
    queryKey: queryKeys.inbox(normalized),
  });
}
