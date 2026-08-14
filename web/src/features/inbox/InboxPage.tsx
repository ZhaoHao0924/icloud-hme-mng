import {
  ArrowLeft,
  ChevronDown,
  Globe2,
  Inbox,
  LoaderCircle,
  RefreshCw,
  Server,
} from "lucide-react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState, type InputHTMLAttributes } from "react";
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
const mobileInboxMediaQuery = "(max-width: 760px)";

function useMediaQuery(query: string) {
  const [matches, setMatches] = useState(() =>
    typeof window.matchMedia === "function" ? window.matchMedia(query).matches : false,
  );

  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const mediaQuery = window.matchMedia(query);
    const updateMatch = () => setMatches(mediaQuery.matches);
    updateMatch();
    mediaQuery.addEventListener("change", updateMatch);
    return () => mediaQuery.removeEventListener("change", updateMatch);
  }, [query]);

  return matches;
}

type DraftFilterInputProps = Omit<
  InputHTMLAttributes<HTMLInputElement>,
  "defaultValue" | "list" | "onBlur" | "onChange" | "onFocus" | "onKeyDown" | "value"
> & {
  options: Array<{ detail?: string; value: string }>;
  value: string;
  onCommit: (value: string) => string | void;
};

function DraftFilterInput({ onCommit, options, value, ...props }: DraftFilterInputProps) {
  const [draft, setDraft] = useState(value);
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const [filtering, setFiltering] = useState(false);
  const lastCommittedValue = useRef(value);
  const inputId = props.id ?? "inbox-filter";
  const optionListId = `${inputId}-options`;
  const filter = filtering ? draft.trim().toLocaleLowerCase() : "";
  const matchingOptions = filter
    ? options.filter(({ detail, value: optionValue }) =>
        `${optionValue} ${detail ?? ""}`.toLocaleLowerCase().includes(filter),
      )
    : options;
  const hasOptions = open && matchingOptions.length > 0;

  function commitValue(nextValue: string) {
    const committedValue = onCommit(nextValue);
    const resolvedValue = typeof committedValue === "string" ? committedValue : nextValue;
    lastCommittedValue.current = resolvedValue;
    setDraft(resolvedValue);
    setFiltering(false);
    setOpen(false);
    setActiveIndex(-1);
  }

  function commitDraft() {
    setFiltering(false);
    setOpen(false);
    setActiveIndex(-1);
    if (draft === lastCommittedValue.current) return;
    commitValue(draft);
  }

  function selectOption(option: { detail?: string; value: string }) {
    commitValue(option.value);
  }

  function moveActiveOption(direction: -1 | 1) {
    setOpen(true);
    setActiveIndex((current) => {
      if (matchingOptions.length === 0) return -1;
      if (current < 0) return direction > 0 ? 0 : matchingOptions.length - 1;
      return (current + direction + matchingOptions.length) % matchingOptions.length;
    });
  }

  const activeOption = activeIndex >= 0 ? matchingOptions[activeIndex] : undefined;

  return (
    <div className="inbox-filter-combobox">
      <input
        {...props}
        aria-activedescendant={activeOption ? `${optionListId}-${activeIndex}` : undefined}
        aria-autocomplete="list"
        aria-controls={hasOptions ? optionListId : undefined}
        aria-expanded={hasOptions}
        role="combobox"
        value={draft}
        onBlur={commitDraft}
        onChange={(event) => {
          setDraft(event.target.value);
          setFiltering(true);
          setOpen(true);
          setActiveIndex(-1);
        }}
        onFocus={() => {
          setFiltering(false);
          setOpen(true);
          setActiveIndex(-1);
        }}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown") {
            event.preventDefault();
            moveActiveOption(1);
            return;
          }
          if (event.key === "ArrowUp") {
            event.preventDefault();
            moveActiveOption(-1);
            return;
          }
          if (event.key === "Escape") {
            if (!open) return;
            event.preventDefault();
            setDraft(value);
            lastCommittedValue.current = value;
            setFiltering(false);
            setOpen(false);
            setActiveIndex(-1);
            return;
          }
          if (event.key === "Enter") {
            event.preventDefault();
            if (activeOption) {
              selectOption(activeOption);
            } else {
              commitDraft();
            }
          }
        }}
      />
      {hasOptions ? (
        <ul className="inbox-filter-options" id={optionListId} role="listbox">
          {matchingOptions.map((option, index) => (
            <li
              aria-selected={index === activeIndex}
              className="inbox-filter-option"
              id={`${optionListId}-${index}`}
              key={option.value}
              role="option"
              onPointerDown={(event) => {
                event.preventDefault();
                selectOption(option);
              }}
            >
              <span className="inbox-filter-option-value">{option.value}</span>
              {option.detail ? (
                <span className="inbox-filter-option-detail">{option.detail}</span>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
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

function looksLikeHtml(value: string) {
  return /<(?:!doctype|html|head|body|style|table|div|p|a)\b/i.test(value);
}

function safeEmailURL(value: string, protocols: string[]) {
  const trimmed = value.trim();
  if (trimmed.startsWith("#")) return trimmed;
  try {
    const parsed = new URL(trimmed);
    return protocols.includes(parsed.protocol) ? trimmed : "";
  } catch {
    return "";
  }
}

function buildEmailHtmlDocument(rawHtml: string) {
  const documentNode = new DOMParser().parseFromString(rawHtml, "text/html");

  documentNode
    .querySelectorAll("script, iframe, object, embed, input, textarea, select, button")
    .forEach((element) => element.remove());
  documentNode.querySelectorAll("form").forEach((form) => form.replaceWith(...form.childNodes));
  documentNode
    .querySelectorAll('meta[http-equiv="refresh"], base')
    .forEach((element) => element.remove());
  documentNode.querySelectorAll<HTMLElement>("*").forEach((element) => {
    for (const attribute of Array.from(element.attributes)) {
      if (attribute.name.toLowerCase().startsWith("on")) {
        element.removeAttribute(attribute.name);
      }
    }
  });
  documentNode.querySelectorAll<HTMLAnchorElement>("a").forEach((anchor) => {
    const href = safeEmailURL(anchor.getAttribute("href") ?? "", [
      "http:",
      "https:",
      "mailto:",
      "tel:",
    ]);
    if (href) {
      anchor.href = href;
      anchor.target = "_blank";
      anchor.rel = "noopener noreferrer";
    } else {
      anchor.removeAttribute("href");
    }
  });
  documentNode.querySelectorAll<HTMLImageElement>("img").forEach((image) => {
    const source = safeEmailURL(image.getAttribute("src") ?? "", [
      "http:",
      "https:",
      "data:",
      "cid:",
    ]);
    if (source) image.src = source;
    else image.removeAttribute("src");
    image.removeAttribute("srcset");
    image.removeAttribute("width");
    image.removeAttribute("height");
    image.style.setProperty("display", "block", "important");
    image.style.setProperty("width", "auto", "important");
    image.style.setProperty("height", "auto", "important");
    image.style.setProperty("aspect-ratio", "auto", "important");
    image.style.setProperty("min-width", "0", "important");
    image.style.setProperty("max-width", "100%", "important");
    image.style.setProperty("max-height", "none", "important");
    image.style.setProperty("object-fit", "contain", "important");
    image.style.setProperty("object-position", "left top", "important");
  });

  const policy = documentNode.createElement("meta");
  policy.httpEquiv = "Content-Security-Policy";
  policy.content =
    "default-src 'none'; img-src http: https: data: cid:; style-src 'unsafe-inline' http: https:; font-src http: https: data:; script-src 'none'; frame-src 'none'; object-src 'none'; form-action 'none'; base-uri 'none'";
  const referrer = documentNode.createElement("meta");
  referrer.name = "referrer";
  referrer.content = "no-referrer";
  const responsiveStyle = documentNode.createElement("style");
  responsiveStyle.textContent = `
    :root { color-scheme: light; }
    *, *::before, *::after { box-sizing: border-box; }
    html, body {
      width: 100% !important;
      max-width: 100% !important;
      min-width: 0 !important;
      height: auto !important;
      min-height: 0 !important;
      margin: 0 !important;
      padding: 0;
      overflow-wrap: anywhere;
    }
    html { overflow-x: hidden !important; overflow-y: auto; }
    body {
      overflow: visible !important;
      padding: 16px;
      color: #202326;
      background: #fff;
      font: 14px/1.55 system-ui, sans-serif;
    }
    body * { max-width: 100% !important; min-width: 0 !important; overflow-wrap: anywhere; }
    img, video, canvas, svg {
      max-width: 100% !important;
      height: auto !important;
      object-fit: contain !important;
      object-position: left top !important;
    }
    table { width: 100% !important; max-width: 100% !important; min-width: 0 !important; table-layout: fixed !important; }
    td, th { max-width: 100% !important; min-width: 0 !important; overflow-wrap: anywhere !important; word-break: break-word !important; }
    pre { max-width: 100% !important; white-space: pre-wrap !important; overflow-wrap: anywhere !important; }
    a, code { overflow-wrap: anywhere; word-break: break-word; }
    a { color: #1463d2; }
  `;
  documentNode.head.prepend(policy, referrer);
  documentNode.head.append(responsiveStyle);

  return `<!doctype html>\n${documentNode.documentElement.outerHTML}`;
}

function EmailHtmlFrame({ body, title }: { body: string; title: string }) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const resizeObserverRef = useRef<ResizeObserver | null>(null);
  const [frameHeight, setFrameHeight] = useState<number | null>(null);

  const readFrameDocument = useCallback(() => {
    try {
      return frameRef.current?.contentDocument ?? null;
    } catch {
      return null;
    }
  }, []);

  const syncFrameLayout = useCallback(() => {
    const frame = frameRef.current;
    const frameDocument = readFrameDocument();
    const frameBody = frameDocument?.body;
    const documentElement = frameDocument?.documentElement;
    if (!frame || !frameDocument || !frameBody || !documentElement) return;

    frameBody.style.setProperty("width", "100%", "important");
    frameBody.style.setProperty("max-width", "100%", "important");
    frameBody.style.setProperty("overflow", "visible", "important");
    frameBody.style.setProperty("transform", "none", "important");
    const bodyRect = frameBody.getBoundingClientRect();
    const viewportWidth = Math.max(frame.clientWidth, documentElement.clientWidth);
    const widestContent = Array.from(frameBody.querySelectorAll("*")).reduce(
      (width, element) => {
        const rect = element.getBoundingClientRect();
        return Math.max(width, rect.right - bodyRect.left, bodyRect.right - rect.left);
      },
      Math.max(documentElement.scrollWidth, frameBody.scrollWidth, bodyRect.width),
    );
    const scale =
      viewportWidth > 0 && widestContent > viewportWidth
        ? Math.min(1, viewportWidth / widestContent)
        : 1;
    if (scale < 1) {
      frameBody.style.setProperty("width", `${widestContent}px`, "important");
      frameBody.style.setProperty("max-width", "none", "important");
    }

    const measuredHeight = [documentElement, frameBody]
      .filter((element): element is HTMLElement => element !== null)
      .reduce(
        (height, element) =>
          Math.max(
            height,
            element.scrollHeight,
            element.offsetHeight,
            Math.ceil(element.getBoundingClientRect().height),
          ),
        0,
      );
    frameBody.style.setProperty("transform-origin", "top left", "important");
    frameBody.style.setProperty("transform", scale < 1 ? `scale(${scale})` : "none", "important");
    const scaledHeight = Math.ceil(measuredHeight * scale);
    if (scaledHeight <= 0) return;
    setFrameHeight((currentHeight) =>
      currentHeight === scaledHeight ? currentHeight : scaledHeight,
    );
  }, [readFrameDocument]);

  const handleFrameLoad = useCallback(() => {
    resizeObserverRef.current?.disconnect();
    resizeObserverRef.current = null;
    syncFrameLayout();

    const frameDocument = readFrameDocument();
    if (!frameDocument || typeof ResizeObserver === "undefined") return;

    const observer = new ResizeObserver(syncFrameLayout);
    observer.observe(frameDocument.documentElement);
    if (frameDocument.body) observer.observe(frameDocument.body);
    resizeObserverRef.current = observer;
  }, [readFrameDocument, syncFrameLayout]);

  useEffect(() => {
    resizeObserverRef.current?.disconnect();
    resizeObserverRef.current = null;
    return () => resizeObserverRef.current?.disconnect();
  }, [body]);

  return (
    <iframe
      className="inbox-html-frame"
      ref={frameRef}
      referrerPolicy="no-referrer"
      sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
      srcDoc={buildEmailHtmlDocument(body)}
      style={frameHeight === null ? undefined : { height: `${frameHeight}px` }}
      title={title}
      onLoad={handleFrameLoad}
    />
  );
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
  onBack,
  onRetryPreview,
  previewError,
  previewPending,
}: {
  message: InboxMessage;
  onBack?: () => void;
  onRetryPreview: () => void;
  previewError: unknown;
  previewPending: boolean;
}) {
  const body = message.body?.trim() || message.preview;
  const isHtml =
    message.content_type?.toLowerCase().startsWith("text/html") === true || looksLikeHtml(body);

  return (
    <section className="table-frame inbox-preview-panel" aria-labelledby="inbox-preview-title">
      {onBack ? (
        <button
          className="button button-secondary inbox-preview-back"
          type="button"
          onClick={onBack}
        >
          <ArrowLeft size={15} aria-hidden="true" />
          返回邮件列表
        </button>
      ) : null}
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
        <span>正文</span>
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
        ) : isHtml ? (
          <EmailHtmlFrame body={body} title={`邮件正文：${messageSubject(message)}`} />
        ) : (
          <div className="inbox-plain-body">{body || "无正文内容"}</div>
        )}
      </div>
    </section>
  );
}

export function InboxPage() {
  const { account } = useAccountDetailContext();
  const navigate = useNavigate();
  const isMobileInbox = useMediaQuery(mobileInboxMediaQuery);
  const [searchParams, setSearchParams] = useSearchParams();
  const accountsQuery = useQuery(accountsQueryOptions());
  const aliasesQuery = useQuery(aliasesQueryOptions(account.id));
  const selectedAlias = searchParams.get("alias") ?? "";
  const days = parseOption(searchParams.get("days"), dayOptions, 7);
  const limit = parseOption(searchParams.get("limit"), limitOptions, 20);
  const aliases = aliasesQuery.data?.aliases ?? [];
  const inboxQueryEnabled = account.id !== "";
  const [inboxRefreshKey, setInboxRefreshKey] = useState(0);
  const selectionContextKey = `${account.id}\u0000${selectedAlias}\u0000${days}\u0000${limit}`;
  const [messageSelection, setMessageSelection] = useState<{
    contextKey: string;
    messageId: string;
  } | null>(null);
  const selectedMessageId =
    messageSelection?.contextKey === selectionContextKey ? messageSelection.messageId : null;
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
    messages.find((message) => message.id === selectedMessageId) ??
    (!isMobileInbox ? messages[0] : null) ??
    null;
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

  function resetMobileScrollPosition() {
    if (!isMobileInbox) return;
    document.documentElement.scrollTop = 0;
    document.body.scrollTop = 0;
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
    <section
      className={`inbox-page${isMobileInbox && selectedMessage ? " inbox-page-message-open" : ""}`}
      aria-labelledby="inbox-page-title"
    >
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
              setMessageSelection(null);
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
            options={accounts.map((candidate) => ({
              detail: candidate.name,
              value: accountFilterValue(candidate),
            }))}
            value={accountFilterValue(account)}
            autoCapitalize="none"
            autoComplete="off"
            inputMode="email"
            spellCheck={false}
            onCommit={commitAccountInput}
          />
        </div>

        <div className="form-field">
          <label htmlFor="inbox-alias">别名</label>
          <DraftFilterInput
            key={`alias-${selectedAlias}`}
            id="inbox-alias"
            options={aliases.map((alias) => ({ detail: alias.label, value: alias.email }))}
            value={selectedAlias}
            aria-describedby="inbox-alias-help"
            autoCapitalize="none"
            autoComplete="off"
            inputMode="email"
            placeholder="全部别名或输入邮箱"
            spellCheck={false}
            onCommit={updateAlias}
          />
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

          {messages.length > 0 ? (
            <div className="inbox-content-grid">
              {!isMobileInbox || !selectedMessage ? (
                <InboxMessageList
                  messages={messages}
                  canLoadMore={inboxQuery.hasNextPage}
                  loadMoreError={loadMoreError}
                  loadingMore={inboxQuery.isFetchingNextPage}
                  selectedMessageId={selectedMessage?.id ?? ""}
                  selectedMessagePreview={selectedMessage?.preview ?? ""}
                  onLoadMore={() => void inboxQuery.fetchNextPage()}
                  onSelect={(messageId) => {
                    setMessageSelection({ contextKey: selectionContextKey, messageId });
                    resetMobileScrollPosition();
                  }}
                />
              ) : null}
              {selectedMessage ? (
                <InboxPreview
                  message={selectedMessage}
                  previewError={selectedMessageNeedsPreview ? selectedMessageQuery.error : null}
                  previewPending={selectedMessageNeedsPreview && selectedMessageQuery.isPending}
                  onBack={
                    isMobileInbox
                      ? () => {
                          setMessageSelection(null);
                          resetMobileScrollPosition();
                        }
                      : undefined
                  }
                  onRetryPreview={() => void selectedMessageQuery.refetch()}
                />
              ) : null}
            </div>
          ) : null}
        </>
      ) : null}
    </section>
  );
}
