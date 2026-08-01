import { Globe2, Inbox, RefreshCw, Server } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";

import { isApiError } from "../../api/client";
import { accountsQueryOptions, aliasesQueryOptions, inboxQueryOptions } from "../../api/queries";
import type { InboxMessage } from "../../api/schemas";
import { EmptyState } from "../../components/EmptyState";
import { LoadingState } from "../../components/LoadingState";
import { useAccountDetailContext } from "../accounts/accountDetailContext";
import { AccountRequestErrorState } from "../security/SessionRecoveryView";

const dayOptions = [1, 3, 7, 14, 30] as const;
const limitOptions = [10, 20, 50] as const;

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
  messages,
  onSelect,
  selectedMessageId,
}: {
  messages: InboxMessage[];
  onSelect: (messageId: string) => void;
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
                <span className="inbox-message-preview">{message.preview || "无预览内容"}</span>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function InboxPreview({ message }: { message: InboxMessage }) {
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
        <p>{message.preview || "无预览内容"}</p>
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
  const selectedAliasExists =
    selectedAlias === "" || aliases.some(({ email }) => email === selectedAlias);
  const inboxQueryEnabled =
    account.id !== "" && (selectedAlias === "" || (aliasesQuery.isSuccess && selectedAliasExists));
  const inboxQuery = useQuery({
    ...inboxQueryOptions({ accountId: account.id, alias: selectedAlias, days, limit }),
    enabled: inboxQueryEnabled,
  });
  const accounts = accountsQuery.data ?? [account];
  const [selectedMessageId, setSelectedMessageId] = useState<string | null>(null);
  const messages = inboxQuery.data?.messages ?? [];
  const selectedMessage =
    messages.find((message) => message.id === selectedMessageId) ?? messages[0] ?? null;
  const method = inboxQuery.data ? readMethodMeta(inboxQuery.data.method) : null;
  const inboxErrorTitle =
    isApiError(inboxQuery.error) &&
    (inboxQuery.error.kind === "timeout" || inboxQuery.error.status === 504)
      ? "读取邮件超时"
      : "收件箱加载失败";

  useEffect(() => {
    if (!selectedAlias || !aliasesQuery.isSuccess || selectedAliasExists) return;
    const nextParams = new URLSearchParams(searchParams);
    nextParams.delete("alias");
    setSearchParams(nextParams, { replace: true });
  }, [aliasesQuery.isSuccess, searchParams, selectedAlias, selectedAliasExists, setSearchParams]);

  function updateAlias(nextAlias: string) {
    const nextParams = new URLSearchParams(searchParams);
    if (nextAlias) {
      nextParams.set("alias", nextAlias);
    } else {
      nextParams.delete("alias");
    }
    setSearchParams(nextParams, { replace: true });
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

  return (
    <section className="inbox-page" aria-labelledby="inbox-page-title">
      <div className="section-heading">
        <div>
          <h3 id="inbox-page-title">邮件收件箱</h3>
          <span className="record-count">
            {inboxQuery.isSuccess ? `${inboxQuery.data.count} 封邮件` : "正在同步"}
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
            onClick={() => void inboxQuery.refetch()}
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
          <select
            id="inbox-account"
            value={account.id}
            onChange={(event) => updateAccount(event.target.value)}
          >
            {accounts.map((candidate) => (
              <option key={candidate.id} value={candidate.id}>
                {candidate.name} · {candidate.icloud_email || candidate.id}
              </option>
            ))}
          </select>
        </div>

        <div className="form-field">
          <label htmlFor="inbox-alias">别名</label>
          <select
            id="inbox-alias"
            value={selectedAlias}
            disabled={aliasesQuery.isPending || aliasesQuery.isError}
            onChange={(event) => updateAlias(event.target.value)}
          >
            <option value="">全部别名</option>
            {!selectedAliasExists ? <option value={selectedAlias}>{selectedAlias}</option> : null}
            {aliases.map((alias) => (
              <option key={alias.anonymousId} value={alias.email}>
                {alias.email}
                {alias.label ? ` · ${alias.label}` : ""}
              </option>
            ))}
          </select>
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

      {inboxQuery.isError ? (
        <AccountRequestErrorState
          accountId={account.id}
          error={inboxQuery.error}
          onRetry={() => void inboxQuery.refetch()}
          title={inboxErrorTitle}
        />
      ) : null}

      {!aliasesQuery.isPending && inboxQuery.isSuccess ? (
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
                selectedMessageId={selectedMessage.id}
                onSelect={setSelectedMessageId}
              />
              <InboxPreview message={selectedMessage} />
            </div>
          ) : null}
        </>
      ) : null}
    </section>
  );
}
