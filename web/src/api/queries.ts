import { infiniteQueryOptions, queryOptions, type InfiniteData } from "@tanstack/react-query";

import { api, type ApiClient, type InboxQuery } from "./client";
import type { Inbox } from "./schemas";

const defaultInboxLimit = 20;
const defaultInboxDays = 7;
const inboxRequestTimeoutMs = 60_000;
const inboxMessageRequestTimeoutMs = 30_000;
const inboxMessageStaleTimeMs = 10 * 60_000;
const inboxStaleTimeMs = 30_000;
const aliasesStaleTimeMs = 30_000;

function normalizedInboxQuery(query: InboxQuery) {
  return {
    accountId: query.accountId,
    alias: query.alias ?? "",
    days: query.days ?? defaultInboxDays,
    limit: query.limit ?? defaultInboxLimit,
  };
}

function normalizedInboxCursor(query: InboxQuery) {
  return query.beforeUid ?? "";
}

export const queryKeys = {
  account: (accountId: string) => ["account", accountId] as const,
  accounts: ["accounts"] as const,
  aliasAutomation: (accountId: string) => ["alias-automation", accountId] as const,
  aliasCreationHistory: (accountId: string) => ["alias-creation-history", accountId] as const,
  aliases: (accountId: string) => ["aliases", accountId] as const,
  health: ["health"] as const,
  operationLogs: ["operation-logs"] as const,
  emailNotification: ["email-notification"] as const,
  webhookNotification: ["webhook-notification"] as const,
  platformAuth: ["platform-auth"] as const,
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
  inboxPage: (query: InboxQuery) =>
    [...queryKeys.inbox(query), normalizedInboxCursor(query)] as const,
  inboxFeed: (query: InboxQuery, refreshKey: number) =>
    [...queryKeys.inbox(query), "feed", refreshKey] as const,
  inboxMessage: (accountId: string, messageId: string) =>
    ["inbox-message", accountId, messageId] as const,
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
    staleTime: aliasesStaleTimeMs,
  });
}

export function aliasAutomationQueryOptions(accountId: string, client: ApiClient = api) {
  return queryOptions({
    enabled: accountId !== "",
    queryFn: ({ signal }) => client.getAliasAutomation(accountId, { signal }),
    queryKey: queryKeys.aliasAutomation(accountId),
  });
}

export function aliasCreationHistoryQueryOptions(accountId: string, client: ApiClient = api) {
  return queryOptions({
    enabled: accountId !== "",
    queryFn: ({ signal }) => client.listAliasCreationHistory(accountId, { signal }),
    queryKey: queryKeys.aliasCreationHistory(accountId),
  });
}

export function healthQueryOptions(client: ApiClient = api) {
  return queryOptions({
    queryFn: ({ signal }) => client.getHealth({ signal }),
    queryKey: queryKeys.health,
  });
}

export function operationLogsQueryOptions(client: ApiClient = api) {
  return queryOptions({
    queryFn: ({ signal }) => client.listOperationLogs({ signal }),
    queryKey: queryKeys.operationLogs,
    staleTime: 10_000,
  });
}

export function emailNotificationQueryOptions(client: ApiClient = api) {
  return queryOptions({
    queryFn: ({ signal }) => client.getEmailNotification({ signal }),
    queryKey: queryKeys.emailNotification,
    staleTime: 30_000,
  });
}

export function webhookNotificationQueryOptions(client: ApiClient = api) {
  return queryOptions({
    queryFn: ({ signal }) => client.getWebhookNotification({ signal }),
    queryKey: queryKeys.webhookNotification,
    staleTime: 30_000,
  });
}

export function platformAuthQueryOptions(client: ApiClient = api) {
  return queryOptions({
    queryFn: ({ signal }) => client.getPlatformAuthSession({ signal }),
    queryKey: queryKeys.platformAuth,
    retry: false,
    staleTime: 60_000,
  });
}

export function inboxQueryOptions(query: InboxQuery, client: ApiClient = api) {
  const normalized = normalizedInboxQuery(query);
  return queryOptions({
    enabled: normalized.accountId !== "",
    queryFn: ({ signal }) =>
      client.listInbox(
        { ...normalized, beforeUid: normalizedInboxCursor(query) || undefined },
        { signal, timeoutMs: inboxRequestTimeoutMs },
      ),
    queryKey: queryKeys.inboxPage(query),
    staleTime: inboxStaleTimeMs,
  });
}

export function inboxInfiniteQueryOptions(
  query: InboxQuery,
  {
    client = api,
    refreshKey = 0,
  }: {
    client?: ApiClient;
    refreshKey?: number;
  } = {},
) {
  const normalized = normalizedInboxQuery(query);
  return infiniteQueryOptions<
    Inbox,
    Error,
    InfiniteData<Inbox>,
    ReturnType<typeof queryKeys.inboxFeed>,
    string
  >({
    enabled: normalized.accountId !== "",
    getNextPageParam: (lastPage) =>
      lastPage.has_more && lastPage.next_cursor !== "" ? lastPage.next_cursor : undefined,
    initialPageParam: "",
    queryFn: ({ pageParam, signal }) =>
      client.listInbox(
        { ...normalized, beforeUid: pageParam === "" ? undefined : pageParam },
        { signal, timeoutMs: inboxRequestTimeoutMs },
      ),
    queryKey: queryKeys.inboxFeed(normalized, refreshKey),
    staleTime: inboxStaleTimeMs,
  });
}

export function inboxMessageQueryOptions(
  accountId: string,
  messageId: string,
  client: ApiClient = api,
) {
  return queryOptions({
    enabled: accountId !== "" && messageId !== "",
    queryFn: ({ signal }) =>
      client.getInboxMessage(accountId, messageId, {
        signal,
        timeoutMs: inboxMessageRequestTimeoutMs,
      }),
    queryKey: queryKeys.inboxMessage(accountId, messageId),
    staleTime: inboxMessageStaleTimeMs,
  });
}
