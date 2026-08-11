import { Fragment, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  Download,
  LoaderCircle,
  RefreshCw,
} from "lucide-react";

import { api, getApiErrorMessage } from "../../api/client";
import { aliasCreationHistoryQueryOptions } from "../../api/queries";
import type { AliasCreationHistoryEntry } from "../../api/schemas";
import { useNotifications } from "../../components/notificationContext";

type AliasCreationHistoryPanelProps = {
  accountId: string;
};

const historyPageSize = 10;

function formatTime(value: string) {
  const time = new Date(value);
  if (Number.isNaN(time.getTime())) return "未知";
  return new Intl.DateTimeFormat("zh-CN", {
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    month: "2-digit",
    year: "numeric",
  }).format(time);
}

function statusLabel(status: AliasCreationHistoryEntry["status"]) {
  switch (status) {
    case "success":
      return "完成";
    case "partial":
      return "部分完成";
    case "skipped":
      return "未创建";
    case "error":
      return "失败";
  }
}

function triggerLabel(trigger: AliasCreationHistoryEntry["trigger"]) {
  switch (trigger) {
    case "manual":
      return "单个创建";
    case "batch":
      return "批量创建";
    case "automation_manual":
      return "手动执行规则";
    case "automation_scheduled":
      return "定时执行规则";
  }
}

function downloadCSV(csv: string) {
  const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.download = "alias-creation-history.csv";
  anchor.href = url;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

export function AliasCreationHistoryPanel({ accountId }: AliasCreationHistoryPanelProps) {
  const { notify } = useNotifications();
  const [pageState, setPageState] = useState<{ key: string | null; page: number }>({
    key: null,
    page: 1,
  });
  const [expandedBatchId, setExpandedBatchId] = useState<string | null>(null);
  const historyQuery = useQuery(aliasCreationHistoryQueryOptions(accountId));
  const exportHistory = useMutation({
    mutationFn: () => api.downloadAliasCreationHistory(accountId),
    onSuccess: (csv) => {
      downloadCSV(csv);
      notify({ title: "创建历史已导出", tone: "success" });
    },
    retry: false,
  });

  const entries = historyQuery.data?.entries ?? [];
  const pageCount = Math.max(1, Math.ceil(entries.length / historyPageSize));
  const pageStateKey = `${accountId}:${historyQuery.data?.count ?? 0}`;
  const currentPage = Math.min(pageState.key === pageStateKey ? pageState.page : 1, pageCount);
  const pagedEntries = entries.slice(
    (currentPage - 1) * historyPageSize,
    currentPage * historyPageSize,
  );
  const pending = exportHistory.isPending || historyQuery.isFetching;

  return (
    <section className="creation-history" aria-labelledby="creation-history-title">
      <div className="section-heading">
        <div>
          <h4 id="creation-history-title">创建历史</h4>
          <span className="record-count">保留最近 {historyQuery.data?.count ?? 0} 个批次</span>
        </div>
        <div className="section-heading-actions">
          <button
            className="icon-button"
            type="button"
            aria-label="刷新创建历史"
            title="刷新创建历史"
            disabled={pending}
            onClick={() => void historyQuery.refetch()}
          >
            <RefreshCw size={17} aria-hidden="true" />
          </button>
          <button
            className="icon-button"
            type="button"
            aria-label="导出创建历史 CSV"
            title="导出创建历史 CSV"
            disabled={exportHistory.isPending || entries.length === 0}
            onClick={() => {
              exportHistory.reset();
              exportHistory.mutate();
            }}
          >
            {exportHistory.isPending ? (
              <LoaderCircle className="button-spinner" size={17} aria-hidden="true" />
            ) : (
              <Download size={17} aria-hidden="true" />
            )}
          </button>
        </div>
      </div>

      {historyQuery.isPending ? (
        <div className="creation-history-empty">正在读取创建历史</div>
      ) : historyQuery.isError ? (
        <div className="form-submit-error" role="alert">
          {getApiErrorMessage(historyQuery.error)}
        </div>
      ) : entries.length === 0 ? (
        <div className="creation-history-empty">暂无创建记录</div>
      ) : (
        <div className="creation-history-table-wrap">
          <table className="creation-history-table">
            <thead>
              <tr>
                <th scope="col">批次</th>
                <th scope="col">来源</th>
                <th scope="col">结果</th>
                <th scope="col">数量</th>
                <th scope="col">时间</th>
                <th scope="col">别名</th>
              </tr>
            </thead>
            <tbody>
              {pagedEntries.map((entry) => {
                const aliasesExpanded = expandedBatchId === entry.batch_id;

                return (
                  <Fragment key={entry.batch_id}>
                    <tr className="creation-history-row">
                      <td className="creation-history-batch">
                        <code>{entry.batch_id}</code>
                      </td>
                      <td className="creation-history-source">{triggerLabel(entry.trigger)}</td>
                      <td className="creation-history-result">
                        <span
                          className={`creation-history-status creation-history-status-${entry.status}`}
                        >
                          {statusLabel(entry.status)}
                        </span>
                        {entry.error ? (
                          <span className="creation-history-error">{entry.error}</span>
                        ) : null}
                      </td>
                      <td className="creation-history-quantity">
                        <span className="creation-history-quantity-value">
                          {entry.created} / {entry.requested}
                        </span>
                        {entry.failed > 0 ? (
                          <span className="creation-history-failed">失败 {entry.failed}</span>
                        ) : null}
                      </td>
                      <td className="creation-history-time">{formatTime(entry.created_at)}</td>
                      <td className="creation-history-alias-cell">
                        {entry.aliases.length > 0 ? (
                          <button
                            className="creation-history-alias-toggle"
                            type="button"
                            aria-expanded={aliasesExpanded}
                            onClick={() => {
                              setExpandedBatchId((current) =>
                                current === entry.batch_id ? null : entry.batch_id,
                              );
                            }}
                          >
                            {aliasesExpanded ? (
                              <ChevronUp size={15} aria-hidden="true" />
                            ) : (
                              <ChevronDown size={15} aria-hidden="true" />
                            )}
                            <span>{entry.aliases.length} 个别名</span>
                          </button>
                        ) : (
                          "-"
                        )}
                      </td>
                    </tr>
                    {aliasesExpanded ? (
                      <tr className="creation-history-alias-row">
                        <td colSpan={6}>
                          <div className="creation-history-alias-panel">
                            <ul className="creation-history-alias-list">
                              {entry.aliases.map((alias) => (
                                <li key={`${entry.batch_id}-${alias.email}`}>
                                  <code>{alias.email}</code>
                                  {alias.label ? <span>{alias.label}</span> : null}
                                </li>
                              ))}
                            </ul>
                          </div>
                        </td>
                      </tr>
                    ) : null}
                  </Fragment>
                );
              })}
            </tbody>
          </table>
          {entries.length > historyPageSize ? (
            <nav className="creation-history-pagination" aria-label="创建历史分页">
              <span className="creation-history-pagination-summary">
                {currentPage} / {pageCount} 页
              </span>
              <div className="creation-history-pagination-controls">
                <button
                  className="icon-button creation-history-pagination-button"
                  type="button"
                  aria-label="上一页"
                  title="上一页"
                  disabled={currentPage === 1}
                  onClick={() => setPageState({ key: pageStateKey, page: currentPage - 1 })}
                >
                  <ChevronLeft size={17} aria-hidden="true" />
                </button>
                <button
                  className="icon-button creation-history-pagination-button"
                  type="button"
                  aria-label="下一页"
                  title="下一页"
                  disabled={currentPage === pageCount}
                  onClick={() => setPageState({ key: pageStateKey, page: currentPage + 1 })}
                >
                  <ChevronRight size={17} aria-hidden="true" />
                </button>
              </div>
            </nav>
          ) : null}
        </div>
      )}

      {exportHistory.isError ? (
        <div className="form-submit-error" role="alert">
          {getApiErrorMessage(exportHistory.error)}
        </div>
      ) : null}
    </section>
  );
}
