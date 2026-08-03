import { ChevronDown, Globe2, Inbox, LoaderCircle, RefreshCw, Server } from "lucide-react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useState, type InputHTMLAttributes } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";

import { getApiErrorMessage, isApiError } from "../../api/client";
import {
  accountsQueryOptions,
  aliasesQueryOptions,
  inboxInfiniteQueryOptions,
  inboxMessageQueryOptions,
} from "../../api/queries";
import type { Account, InboxMessage } from "../../api/schemas";
import { EmptyState } from "../../components/EmptyState";
import { LoadingState } from "../../components/LoadingState";
import { useAccountDetailContext } from "../accounts/accountDetailContext";
import { AccountRequestErrorState } from "../security/SessionRecoveryView";

const dayOptions = [1, 3, 7, 14, 30] as const;
const limitOptions = [10, 20, 50] as const;

type DraftFilterInputProps = Omit<
  InputHTMLAttributes<HTMLInputElement>,
  "defaultValue" | "onBlur" | "onChange" | "onKeyDown" | "value"
> & {
  value: string;
  onCommit: (value: string) => string | void;
};

function DraftFilterInput({ onCommit, value, ...props }: DraftFilterInputProps) {
  const [draft, setDraft] = useState(value);

  function commitDraft() {
    const committedValue = onCommit(draft);
    if (typeof committedValue === "string") {
      setDraft(committedValue);
    }
  }

  return (
    <input
      {...props}
      value={draft}
      onBlur={commitDraft}
      onChange={(event) => setDraft(event.target.value)}
      onKeyDown={(event) => {
        if (event.key === "Enter") {
          event.preventDefault();
          commitDraft();
        }
      }}
    />
  );
}

function parseOption<T extends readonly number[]>(
  value: string | null,
  options: T,
  fallback: T[number],
) {
  const parsed = Number(value);
  return options.includes(parsed as T[number]) ? (parsed as T[number]) : fallback;
}

function accountPath(accountId: string, search: string) {
  const suffix = search ? `?${search}` : "";
  return `/accounts/${encodeURIComponent(accountId)}/inbox${suffix}`;
}

function accountFilterValue(account: Pick<Account, "icloud_email" | "id">) {
  return account.icloud_email.trim() || account.id;
}

function formatInboxDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "未知时间";
  return new Intl.DateTimeFormat("zh-CN", {
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    month: "2-digit",
  }).format(date);
}

function messageSubject(message: InboxMessage) {
  return message.subject || "（无主题）";
}

function readMethodMeta(method: "imap" | "web_api") {
  return method === "imap"
    ? { label: "IMAP", value: "imap" }
    : { label: "Web API", value: "web-api" };
}

function InboxMessageList({
  canLoadMore,
  loadMoreError,
  messages,
  onLoadMore,
  onSelect,
  loadingMore,
  selectedMessagePreview,
  selectedMessageId,
}: {
  canLoadMore: boolean;
  loadMoreError: unknown;
  messages: InboxMessage[];
  onLoadMore: () => void;
  onSelect: (messageId: string) => void;
  loadingMore: boolean;
  selectedMessagePreview: string;
  selectedMessageId: string;
}) {
  return (
    <div className="table-frame inbox-message-panel">
      <h4 className="visually-hidden" id="inbox-message-list-title">
        邮件摘要列表
      </h4>
      <ul aria-labelledby="inbox-message-list-title" className="inbox-message-list">
        {messages.map((message) => {
          const selected = message.id === selectedMessageId;
          const preview = selected ? selectedMessagePreview || message.preview : message.preview;
          return (
            <li key={message.id}>
              <button
                aria-label={`选择邮件 ${messageSubject(message)}`}
                aria-pressed={selected}
                className={`inbox-message-item${selected ? " inbox-message-item-selected" : ""}`}
                type="button"
                onClick={() => onSelect(message.id)}
              >
                <span className="inbox-message-topline">
                  <span className="inbox-message-from">{message.from || "未知发件人"}</span>
                  <time dateTime={message.date}>{formatInboxDate(message.date)}</time>
                </span>
                <strong>{messageSubject(message)}</strong>
                <span className="inbox-message-to">收件地址：{message.to || "未知"}</span>
                <span className="inbox-message-preview">{preview || "正文尚未加载"}</span>
              </button>
            </li>
          );
        })}
      </ul>
      {canLoadMore || loadMoreError ? (
        <div className="inbox-load-more">
          {loadMoreError ? (
            <span className="inbox-load-more-error" role="alert">
              {getApiErrorMessage(loadMoreError)}
            </span>
          ) : null}
          {canLoadMore ? (
            <button
              className="button button-secondary inbox-load-more-button"
              disabled={loadingMore}
              type="button"
              onClick={onLoadMore}
            >
              {loadingMore ? (
                <LoaderCircle className="button-spinner" size={15} aria-hidden="true" />
              ) : (
                <ChevronDown size={15} aria-hidden="true" />
              )}
              {loadingMore ? "正在加载更多邮件" : "加载更多邮件"}
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function InboxPreview({
  message,
  onRetryPreview,
  previewError,
  previewPending,
}: {
  message: InboxMessage;
  onRetryPreview: () => void;
  previewError: unknown;
  previewPending: boolean;
}) {
  return (
    <section className="table-frame inbox-preview-panel" aria-labelledby="inbox-preview-title">
      <div className="inbox-preview-heading">
        <span className="record-count">邮件摘要</span>
        <h4 id="inbox-preview-title">{messageSubject(message)}</h4>
      </div>
      <dl className="inbox-preview-meta">
        <div>
          <dt>发件人</dt>
          <dd>{message.from || "未知发件人"}</dd>
        </div>
        <div>
          <dt>收件地址</dt>
          <dd>{message.to || "未知"}</dd>
        </div>
        <div>
          <dt>时间</dt>
          <dd>
            <time dateTime={message.date}>{formatInboxDate(message.date)}</time>
          </dd>
        </div>
      </dl>
      <div className="inbox-preview-copy">
        <span>预览</span>
        {previewPending ? (
          <div className="inbox-preview-loading" role="status" aria-live="polite">
            <LoaderCircle className="loading-state-icon" size={18} aria-hidden="true" />
            <span>正在读取邮件内容</span>
          </div>
        ) : previewError ? (
          <div className="inbox-preview-error" role="alert">
            <span>{getApiErrorMessage(previewError)}</span>
            <button className="button button-secondary" type="button" onClick={onRetryPreview}>
              <RefreshCw size={14} aria-hidden="true" />
              重新加载
            </button>
          </div>
        ) : (
          <p>{message.preview || "无预览内容"}</p>
        )}
      </div>
    </section>
  );
}

export function InboxPage() {
  const { account } = useAccountDetailContext();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const accountsQuery = useQuery(accountsQueryOptions());
  const aliasesQuery = useQuery(aliasesQueryOptions(account.id));
  const selectedAlias = searchParams.get("alias") ?? "";
  const days = parseOption(searchParams.get("days"), dayOptions, 7);
  const limit = parseOption(searchParams.get("limit"), limitOptions, 20);
  const aliases = aliasesQuery.data?.aliases ?? [];
  const inboxQueryEnabled = account.id !== "";
  const [inboxRefreshKey, setInboxRefreshKey] = useState(0);
  const [selectedMessageId, setSelectedMessageId] = useState<string | null>(null);
  const inboxQuery = useInfiniteQuery({
    ...inboxInfiniteQueryOptions(
      { accountId: account.id, alias: selectedAlias, days, limit },
      { refreshKey: inboxRefreshKey },
    ),
    enabled: inboxQueryEnabled,
  });
  const accounts = accountsQuery.data ?? [account];
  const inboxPages = inboxQuery.data?.pages ?? [];
  const messages = inboxPages.flatMap((page) => page.messages);
  const hasInboxData = inboxQuery.data !== undefined;
  const loadMoreError = inboxQuery.isFetchNextPageError ? inboxQuery.error : null;
  const selectedMessageSummary =
    messages.find((message) => message.id === selectedMessageId) ?? messages[0] ?? null;
  const selectedMessageNeedsPreview =
    inboxPages[0]?.method === "imap" && selectedMessageSummary?.preview === "";
  const selectedMessageQuery = useQuery({
    ...inboxMessageQueryOptions(account.id, selectedMessageSummary?.id ?? ""),
    enabled: selectedMessageNeedsPreview,
  });
  const selectedMessage = selectedMessageNeedsPreview
    ? (selectedMessageQuery.data ?? selectedMessageSummary)
    : selectedMessageSummary;
  const method = inboxPages[0] ? readMethodMeta(inboxPages[0].method) : null;
  const inboxErrorTitle =
    isApiError(inboxQuery.error) &&
    (inboxQuery.error.kind === "timeout" || inboxQuery.error.status === 504)
      ? "读取邮件超时"
      : "收件箱加载失败";

  function updateAlias(nextAlias: string) {
    nextAlias = nextAlias.trim();
    const nextParams = new URLSearchParams(searchParams);
    if (nextAlias) {
      nextParams.set("alias", nextAlias);
    } else {
      nextParams.delete("alias");
    }
    setSearchParams(nextParams, { replace: true });
    return nextAlias;
  }

  function updateOption(parameter: "days" | "limit", value: number, defaultValue: number) {
    const nextParams = new URLSearchParams(searchParams);
    if (value === defaultValue) {
      nextParams.delete(parameter);
    } else {
      nextParams.set(parameter, String(value));
    }
    setSearchParams(nextParams, { replace: true });
  }

  function updateAccount(nextAccountId: string) {
    if (!nextAccountId || nextAccountId === account.id) return;
    const nextParams = new URLSearchParams(searchParams);
    nextParams.delete("alias");
    navigate(accountPath(nextAccountId, nextParams.toString()), { replace: true });
  }

  function commitAccountInput(inputValue: string) {
    const value = inputValue.trim();
    const matchedAccount = accounts.find(
      (candidate) =>
        candidate.id === value ||
        candidate.name === value ||
        candidate.icloud_email.toLowerCase() === value.toLowerCase(),
    );
    if (!matchedAccount) return accountFilterValue(account);
    updateAccount(matchedAccount.id);
    return accountFilterValue(matchedAccount);
  }

  return (
    <section className="inbox-page" aria-labelledby="inbox-page-title">
      <div className="section-heading">
        <div>
          <h3 id="inbox-page-title">邮件收件箱</h3>
          <span className="record-count">
            {hasInboxData ? `${messages.length} 封邮件` : "正在同步"}
          </span>
        </div>
        <div className="inbox-heading-actions">
          {method ? (
            <span
              aria-label={`实际读取方式：${method.label}`}
              className={`inbox-method-chip inbox-method-${method.value}`}
            >
              {method.value === "imap" ? (
                <Server size={14} aria-hidden="true" />
              ) : (
                <Globe2 size={14} aria-hidden="true" />
              )}
              {method.label}
            </span>
          ) : null}
          <button
            aria-label={inboxQuery.isFetching ? "正在刷新收件箱" : "刷新收件箱"}
            className="icon-button inbox-refresh-button"
            disabled={inboxQuery.isFetching || !inboxQueryEnabled}
            title="刷新收件箱"
            type="button"
            onClick={() => {
              setSelectedMessageId(null);
              setInboxRefreshKey((value) => value + 1);
            }}
          >
            <RefreshCw
              className={inboxQuery.isFetching ? "button-spinner" : undefined}
              size={16}
              aria-hidden="true"
            />
          </button>
        </div>
      </div>

      <div className="inbox-toolbar" aria-label="收件箱筛选">
        <div className="form-field">
          <label htmlFor="inbox-account">账户</label>
          <DraftFilterInput
            key={`account-${account.id}-${accountFilterValue(account)}`}
            id="inbox-account"
            list="inbox-account-options"
            value={accountFilterValue(account)}
            autoCapitalize="none"
            autoComplete="off"
            inputMode="email"
            spellCheck={false}
            onCommit={commitAccountInput}
          />
          <datalist id="inbox-account-options">
            {accounts.map((candidate) => (
              <option
                key={candidate.id}
                label={candidate.name}
                value={accountFilterValue(candidate)}
              />
            ))}
          </datalist>
        </div>

        <div className="form-field">
          <label htmlFor="inbox-alias">别名</label>
          <DraftFilterInput
            key={`alias-${selectedAlias}`}
            id="inbox-alias"
            list="inbox-alias-options"
            value={selectedAlias}
            aria-describedby="inbox-alias-help"
            autoCapitalize="none"
            autoComplete="off"
            inputMode="email"
            placeholder="全部别名或输入邮箱"
            spellCheck={false}
            onCommit={updateAlias}
          />
          <datalist id="inbox-alias-options">
            {aliases.map((alias) => (
              <option key={alias.anonymousId} value={alias.email}>
                {alias.email}
                {alias.label ? ` · ${alias.label}` : ""}
              </option>
            ))}
          </datalist>
        </div>

        <div className="form-field">
          <label htmlFor="inbox-days">时间范围</label>
          <select
            id="inbox-days"
            value={days}
            onChange={(event) => updateOption("days", Number(event.target.value), 7)}
          >
            {dayOptions.map((value) => (
              <option key={value} value={value}>
                近 {value} 天
              </option>
            ))}
          </select>
        </div>

        <div className="form-field">
          <label htmlFor="inbox-limit">数量</label>
          <select
            id="inbox-limit"
            value={limit}
            onChange={(event) => updateOption("limit", Number(event.target.value), 20)}
          >
            {limitOptions.map((value) => (
              <option key={value} value={value}>
                {value} 封
              </option>
            ))}
          </select>
        </div>

        <span className="inbox-filter-help" id="inbox-alias-help">
          别名支持从候选中选择，也可以手动输入完整邮箱；清空表示全部别名。
        </span>
      </div>

      {aliasesQuery.isPending || inboxQuery.isPending ? (
        <div className="table-frame inbox-query-state">
          <LoadingState label="正在读取收件箱" />
        </div>
      ) : null}

      {aliasesQuery.isError ? (
        <AccountRequestErrorState
          accountId={account.id}
          error={aliasesQuery.error}
          onRetry={() => void aliasesQuery.refetch()}
          title="别名筛选加载失败"
        />
      ) : null}

      {inboxQuery.isError && messages.length === 0 ? (
        <AccountRequestErrorState
          accountId={account.id}
          error={inboxQuery.error}
          onRetry={() => void inboxQuery.refetch()}
          title={inboxErrorTitle}
        />
      ) : null}

      {!aliasesQuery.isPending && hasInboxData ? (
        <>
          {messages.length === 0 ? (
            <div className="table-frame inbox-query-state">
              <EmptyState
                description="当前筛选范围内没有邮件。"
                icon={<Inbox size={22} />}
                title="暂无匹配邮件"
              />
            </div>
          ) : null}

          {selectedMessage ? (
            <div className="inbox-content-grid">
              <InboxMessageList
                messages={messages}
                canLoadMore={inboxQuery.hasNextPage}
                loadMoreError={loadMoreError}
                loadingMore={inboxQuery.isFetchingNextPage}
                selectedMessageId={selectedMessage.id}
                selectedMessagePreview={selectedMessage.preview}
                onLoadMore={() => void inboxQuery.fetchNextPage()}
                onSelect={setSelectedMessageId}
              />
              <InboxPreview
                message={selectedMessage}
                previewError={selectedMessageNeedsPreview ? selectedMessageQuery.error : null}
                previewPending={selectedMessageNeedsPreview && selectedMessageQuery.isPending}
                onRetryPreview={() => void selectedMessageQuery.refetch()}
              />
            </div>
          ) : null}
        </>
      ) : null}
    </section>
  );
}
