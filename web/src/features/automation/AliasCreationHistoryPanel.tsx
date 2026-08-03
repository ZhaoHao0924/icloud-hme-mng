import { useMutation, useQuery } from "@tanstack/react-query";
import { Download, LoaderCircle, RefreshCw } from "lucide-react";

import { api, getApiErrorMessage } from "../../api/client";
import { aliasCreationHistoryQueryOptions } from "../../api/queries";
import type { AliasCreationHistoryEntry } from "../../api/schemas";
import { useNotifications } from "../../components/notificationContext";

type AliasCreationHistoryPanelProps = {
  accountId: string;
};

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
              {entries.map((entry) => (
                <tr key={entry.batch_id}>
                  <td>
                    <code>{entry.batch_id}</code>
                  </td>
                  <td>{triggerLabel(entry.trigger)}</td>
                  <td>
                    <span
                      className={`creation-history-status creation-history-status-${entry.status}`}
                    >
                      {statusLabel(entry.status)}
                    </span>
                    {entry.error ? (
                      <span className="creation-history-error">{entry.error}</span>
                    ) : null}
                  </td>
                  <td>
                    {entry.created} / {entry.requested}
                    {entry.failed > 0 ? `，失败 ${entry.failed}` : ""}
                  </td>
                  <td>{formatTime(entry.created_at)}</td>
                  <td>
                    {entry.aliases.length > 0 ? (
                      <details className="creation-history-aliases">
                        <summary>{entry.aliases.length} 个别名</summary>
                        <ul>
                          {entry.aliases.map((alias) => (
                            <li key={`${entry.batch_id}-${alias.email}`}>
                              <code>{alias.email}</code>
                              {alias.label ? <span>{alias.label}</span> : null}
                            </li>
                          ))}
                        </ul>
                      </details>
                    ) : (
                      "-"
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
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
