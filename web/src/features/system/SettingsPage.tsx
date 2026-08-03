import {
  CheckCircle2,
  CircleAlert,
  Database,
  Info,
  LoaderCircle,
  Mail,
  RefreshCw,
  Save,
  Send,
  Server,
  ShieldAlert,
  Webhook,
} from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { api, getApiErrorMessage } from "../../api/client";
import {
  emailNotificationQueryOptions,
  healthQueryOptions,
  operationLogsQueryOptions,
  queryKeys,
  webhookNotificationQueryOptions,
} from "../../api/queries";
import type { OperationLogEntry } from "../../api/schemas";
import { ErrorState } from "../../components/ErrorState";
import { LoadingState } from "../../components/LoadingState";
import { useNotifications } from "../../components/notificationContext";

function healthStatusMeta(status: "ok" | "degraded") {
  return status === "ok"
    ? { label: "正常", tone: "success" as const }
    : { label: "降级", tone: "warning" as const };
}

function operationLogLevelMeta(level: OperationLogEntry["level"]) {
  switch (level) {
    case "error":
      return { Icon: CircleAlert, label: "错误", tone: "danger" as const };
    case "warning":
      return { Icon: ShieldAlert, label: "警告", tone: "warning" as const };
    default:
      return { Icon: Info, label: "信息", tone: "neutral" as const };
  }
}

function formatOperationLogTime(timestamp: string) {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return timestamp;
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "medium",
  }).format(date);
}

function formatOperationLogDuration(durationMS: number) {
  return durationMS >= 1_000 ? `${(durationMS / 1_000).toFixed(1)} s` : `${durationMS} ms`;
}

export function SettingsPage() {
  const queryClient = useQueryClient();
  const { notify } = useNotifications();
  const healthQuery = useQuery(healthQueryOptions());
  const operationLogsQuery = useQuery(operationLogsQueryOptions());
  const emailNotificationQuery = useQuery(emailNotificationQueryOptions());
  const webhookNotificationQuery = useQuery(webhookNotificationQueryOptions());
  const [emailDraft, setEmailDraft] = useState<{
    authorizationCode: string;
    enabled: boolean;
    senderEmail: string;
    recipientEmail: string;
  } | null>(null);
  const emailForm = emailDraft ?? {
    authorizationCode: "",
    enabled: emailNotificationQuery.data?.enabled ?? false,
    senderEmail: emailNotificationQuery.data?.sender_email ?? "",
    recipientEmail: emailNotificationQuery.data?.recipient_email ?? "",
  };
  const [webhookDraft, setWebhookDraft] = useState<{
    enabled: boolean;
    secret: string;
    url: string;
  } | null>(null);
  const webhookForm = webhookDraft ?? {
    enabled: webhookNotificationQuery.data?.enabled ?? false,
    secret: "",
    url: webhookNotificationQuery.data?.url ?? "",
  };

  const updateEmailNotificationMutation = useMutation({
    mutationFn: () => api.updateEmailNotification(emailForm),
    onSuccess: async (result) => {
      setEmailDraft((current) => ({
        ...(current ?? emailForm),
        authorizationCode: "",
      }));
      await queryClient.invalidateQueries({ queryKey: queryKeys.emailNotification });
      notify({
        title: result.enabled ? "163 邮箱通知已启用" : "163 邮箱通知设置已保存",
        tone: "success",
      });
    },
  });

  const testEmailNotificationMutation = useMutation({
    mutationFn: () => api.testEmailNotification(),
    onSuccess: (result) => {
      notify({ title: result.message, tone: "success" });
    },
  });
  const updateWebhookNotificationMutation = useMutation({
    mutationFn: () => api.updateWebhookNotification(webhookForm),
    onSuccess: async (result) => {
      setWebhookDraft((current) => ({
        ...(current ?? webhookForm),
        secret: "",
      }));
      await queryClient.invalidateQueries({ queryKey: queryKeys.webhookNotification });
      notify({
        title: result.enabled ? "Webhook 通知已启用" : "Webhook 通知设置已保存",
        tone: "success",
      });
    },
  });

  const testWebhookNotificationMutation = useMutation({
    mutationFn: () => api.testWebhookNotification(),
    onSuccess: (result) => {
      notify({ title: result.message, tone: "success" });
    },
  });
  const reloadMutation = useMutation({
    mutationFn: () => api.reloadConfig(),
    onSuccess: async (result) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.health }),
        queryClient.invalidateQueries({ queryKey: queryKeys.accounts }),
      ]);
      notify({
        title: result.message,
        tone: "success",
      });
    },
  });
  const health = healthQuery.data;
  const status = health ? healthStatusMeta(health.status) : null;

  return (
    <section className="settings-page" aria-labelledby="settings-page-title">
      <div className="section-heading">
        <div>
          <h2 id="settings-page-title">系统设置</h2>
          <span className="record-count">服务状态与配置管理</span>
        </div>
      </div>

      <section className="settings-section" aria-labelledby="settings-health-title">
        <div className="settings-section-heading">
          <div>
            <h3 id="settings-health-title">服务健康</h3>
            <p>状态来自服务端实时检查。</p>
          </div>
          <Server size={20} aria-hidden="true" />
        </div>

        {healthQuery.isPending ? <LoadingState label="正在检查服务状态" /> : null}
        {healthQuery.isError ? (
          <ErrorState
            action={
              <button
                className="button button-secondary"
                type="button"
                onClick={() => void healthQuery.refetch()}
              >
                <RefreshCw size={14} aria-hidden="true" />
                重新检查
              </button>
            }
            description={getApiErrorMessage(healthQuery.error)}
            title="健康检查失败"
          />
        ) : null}
        {healthQuery.isSuccess && health && status ? (
          <dl className="settings-health-list">
            <div className="settings-health-item">
              <dt>服务</dt>
              <dd>
                <Server size={15} aria-hidden="true" />
                {health.service}
              </dd>
            </div>
            <div className="settings-health-item">
              <dt>状态</dt>
              <dd>
                <span className={`status-chip status-chip-${status.tone}`}>
                  {status.tone === "success" ? (
                    <CheckCircle2 size={14} aria-hidden="true" />
                  ) : (
                    <ShieldAlert size={14} aria-hidden="true" />
                  )}
                  {status.label}
                </span>
              </dd>
            </div>
            <div className="settings-health-item">
              <dt>版本</dt>
              <dd>{health.version}</dd>
            </div>
            <div className="settings-health-item">
              <dt>账户配置</dt>
              <dd>
                <Database size={15} aria-hidden="true" />
                {health.config_available ? "可用" : "不可用"}
              </dd>
            </div>
            <div className="settings-health-item">
              <dt>配置位置</dt>
              <dd>服务端本地配置</dd>
            </div>
          </dl>
        ) : null}
      </section>

      <section className="settings-section" aria-labelledby="settings-email-title">
        <div className="settings-section-heading">
          <div>
            <h3 id="settings-email-title">163 邮箱通知</h3>
            <p>通过 163 SMTP 发送自动化结果和会话失效提醒，收件地址必须是 QQ 邮箱。</p>
          </div>
          <Mail size={20} aria-hidden="true" />
        </div>

        {emailNotificationQuery.isPending ? <LoadingState label="正在读取 163 邮箱设置" /> : null}
        {emailNotificationQuery.isError ? (
          <div className="settings-log-error" role="alert">
            {getApiErrorMessage(emailNotificationQuery.error)}
          </div>
        ) : null}
        {emailNotificationQuery.isSuccess && emailNotificationQuery.data ? (
          <form
            className="settings-email-form"
            onSubmit={(event) => {
              event.preventDefault();
              updateEmailNotificationMutation.mutate();
            }}
          >
            <div className="settings-email-provider">
              <span>固定服务</span>
              <strong>
                {emailNotificationQuery.data.smtp_host}:{emailNotificationQuery.data.smtp_port}
              </strong>
              <span className="status-chip status-chip-neutral">163 Mail → QQ</span>
            </div>

            <div className="settings-email-grid">
              <div className="form-field">
                <label htmlFor="email-notification-sender">163 发件邮箱</label>
                <input
                  id="email-notification-sender"
                  autoComplete="email"
                  type="email"
                  value={emailForm.senderEmail}
                  onChange={(event) =>
                    setEmailDraft((current) => ({
                      ...(current ?? emailForm),
                      senderEmail: event.target.value,
                    }))
                  }
                />
              </div>
              <div className="form-field">
                <label htmlFor="email-notification-recipient">QQ 收件邮箱</label>
                <input
                  id="email-notification-recipient"
                  autoComplete="email"
                  type="email"
                  value={emailForm.recipientEmail}
                  onChange={(event) =>
                    setEmailDraft((current) => ({
                      ...(current ?? emailForm),
                      recipientEmail: event.target.value,
                    }))
                  }
                />
              </div>
              <div className="form-field settings-email-code-field">
                <label htmlFor="email-notification-code">163 邮箱授权码</label>
                <input
                  id="email-notification-code"
                  autoComplete="new-password"
                  placeholder={
                    emailNotificationQuery.data.configured ? "已保存，留空保持不变" : "请输入授权码"
                  }
                  type="password"
                  value={emailForm.authorizationCode}
                  onChange={(event) =>
                    setEmailDraft((current) => ({
                      ...(current ?? emailForm),
                      authorizationCode: event.target.value,
                    }))
                  }
                />
              </div>
            </div>

            <label className="settings-email-toggle">
              <input
                aria-label="启用邮件通知"
                type="checkbox"
                checked={emailForm.enabled}
                onChange={(event) =>
                  setEmailDraft((current) => ({
                    ...(current ?? emailForm),
                    enabled: event.target.checked,
                  }))
                }
              />
              <span>
                <strong>启用邮件通知</strong>
                <small>发送失败不会阻塞别名自动化任务。</small>
              </span>
            </label>

            {updateEmailNotificationMutation.isError ? (
              <div className="form-submit-error" role="alert">
                {getApiErrorMessage(updateEmailNotificationMutation.error)}
              </div>
            ) : null}
            {testEmailNotificationMutation.isError ? (
              <div className="form-submit-error" role="alert">
                {getApiErrorMessage(testEmailNotificationMutation.error)}
              </div>
            ) : null}

            <div className="settings-email-actions">
              <button
                className="button button-secondary"
                type="button"
                disabled={
                  !emailNotificationQuery.data.configured ||
                  updateEmailNotificationMutation.isPending ||
                  testEmailNotificationMutation.isPending
                }
                onClick={() => testEmailNotificationMutation.mutate()}
              >
                {testEmailNotificationMutation.isPending ? (
                  <LoaderCircle className="button-spinner" size={15} aria-hidden="true" />
                ) : (
                  <Send size={15} aria-hidden="true" />
                )}
                {testEmailNotificationMutation.isPending ? "正在发送" : "发送测试邮件"}
              </button>
              <button
                className="button button-primary"
                type="submit"
                disabled={
                  updateEmailNotificationMutation.isPending ||
                  testEmailNotificationMutation.isPending
                }
              >
                {updateEmailNotificationMutation.isPending ? (
                  <LoaderCircle className="button-spinner" size={15} aria-hidden="true" />
                ) : (
                  <Save size={15} aria-hidden="true" />
                )}
                {updateEmailNotificationMutation.isPending ? "正在保存" : "保存 163 邮箱设置"}
              </button>
            </div>
          </form>
        ) : null}
      </section>

      <section className="settings-section" aria-labelledby="settings-webhook-title">
        <div className="settings-section-heading">
          <div>
            <h3 id="settings-webhook-title">Webhook 通知</h3>
            <p>将自动化结果和 iCloud 会话失效事件投递到指定 HTTPS 地址。</p>
          </div>
          <Webhook size={20} aria-hidden="true" />
        </div>

        {webhookNotificationQuery.isPending ? <LoadingState label="正在读取 Webhook 设置" /> : null}
        {webhookNotificationQuery.isError ? (
          <div className="settings-log-error" role="alert">
            {getApiErrorMessage(webhookNotificationQuery.error)}
          </div>
        ) : null}
        {webhookNotificationQuery.isSuccess && webhookNotificationQuery.data ? (
          <form
            className="settings-email-form"
            onSubmit={(event) => {
              event.preventDefault();
              updateWebhookNotificationMutation.mutate();
            }}
          >
            <div className="settings-email-provider">
              <span>签名方式</span>
              <strong>HMAC-SHA256</strong>
              <span className="status-chip status-chip-neutral">HTTPS</span>
            </div>

            <div className="settings-webhook-grid">
              <div className="form-field">
                <label htmlFor="webhook-notification-url">Webhook URL</label>
                <input
                  id="webhook-notification-url"
                  autoComplete="url"
                  placeholder="https://example.com/hooks/icloud"
                  type="url"
                  value={webhookForm.url}
                  onChange={(event) =>
                    setWebhookDraft((current) => ({
                      ...(current ?? webhookForm),
                      url: event.target.value,
                    }))
                  }
                />
              </div>
              <div className="form-field">
                <label htmlFor="webhook-notification-secret">签名密钥</label>
                <input
                  id="webhook-notification-secret"
                  autoComplete="new-password"
                  placeholder={
                    webhookNotificationQuery.data.configured
                      ? "已保存，留空保持不变"
                      : "请输入签名密钥"
                  }
                  type="password"
                  value={webhookForm.secret}
                  onChange={(event) =>
                    setWebhookDraft((current) => ({
                      ...(current ?? webhookForm),
                      secret: event.target.value,
                    }))
                  }
                />
              </div>
            </div>

            <label className="settings-email-toggle">
              <input
                aria-label="启用 Webhook 通知"
                type="checkbox"
                checked={webhookForm.enabled}
                onChange={(event) =>
                  setWebhookDraft((current) => ({
                    ...(current ?? webhookForm),
                    enabled: event.target.checked,
                  }))
                }
              />
              <span>
                <strong>启用 Webhook 通知</strong>
                <small>投递失败不会阻塞别名自动化任务。</small>
              </span>
            </label>

            {updateWebhookNotificationMutation.isError ? (
              <div className="form-submit-error" role="alert">
                {getApiErrorMessage(updateWebhookNotificationMutation.error)}
              </div>
            ) : null}
            {testWebhookNotificationMutation.isError ? (
              <div className="form-submit-error" role="alert">
                {getApiErrorMessage(testWebhookNotificationMutation.error)}
              </div>
            ) : null}

            <div className="settings-email-actions">
              <button
                className="button button-secondary"
                type="button"
                disabled={
                  !webhookNotificationQuery.data.configured ||
                  updateWebhookNotificationMutation.isPending ||
                  testWebhookNotificationMutation.isPending
                }
                onClick={() => testWebhookNotificationMutation.mutate()}
              >
                {testWebhookNotificationMutation.isPending ? (
                  <LoaderCircle className="button-spinner" size={15} aria-hidden="true" />
                ) : (
                  <Send size={15} aria-hidden="true" />
                )}
                {testWebhookNotificationMutation.isPending ? "正在发送" : "发送测试"}
              </button>
              <button
                className="button button-primary"
                type="submit"
                disabled={
                  updateWebhookNotificationMutation.isPending ||
                  testWebhookNotificationMutation.isPending
                }
              >
                {updateWebhookNotificationMutation.isPending ? (
                  <LoaderCircle className="button-spinner" size={15} aria-hidden="true" />
                ) : (
                  <Save size={15} aria-hidden="true" />
                )}
                {updateWebhookNotificationMutation.isPending ? "正在保存" : "保存 Webhook 设置"}
              </button>
            </div>
          </form>
        ) : null}
      </section>

      <section className="settings-section" aria-labelledby="settings-operation-logs-title">
        <div className="settings-section-heading">
          <div>
            <h3 id="settings-operation-logs-title">操作日志</h3>
            <p>保留最近 7 天的关键操作与失败记录，过期后自动清理。</p>
          </div>
          <button
            className="icon-button"
            type="button"
            aria-label="刷新日志"
            title="刷新日志"
            disabled={operationLogsQuery.isFetching}
            onClick={() => void operationLogsQuery.refetch()}
          >
            <RefreshCw
              className={operationLogsQuery.isFetching ? "button-spinner" : undefined}
              size={16}
              aria-hidden="true"
            />
          </button>
        </div>

        {operationLogsQuery.isPending ? <LoadingState label="正在读取日志" /> : null}
        {operationLogsQuery.isError ? (
          <div className="settings-log-error" role="alert">
            {getApiErrorMessage(operationLogsQuery.error)}
          </div>
        ) : null}
        {operationLogsQuery.isSuccess && operationLogsQuery.data ? (
          <>
            <span className="record-count settings-log-count">
              最近 {operationLogsQuery.data.retention_days} 天 · {operationLogsQuery.data.count} 条
            </span>
            {operationLogsQuery.data.entries.length === 0 ? (
              <p className="settings-log-empty">暂无操作日志</p>
            ) : (
              <ol className="settings-log-list" aria-label="最近操作记录">
                {operationLogsQuery.data.entries.map((entry, index) => {
                  const level = operationLogLevelMeta(entry.level);
                  return (
                    <li key={`${entry.timestamp}-${entry.operation}-${index}`}>
                      <div className="settings-log-entry-main">
                        <time dateTime={entry.timestamp}>
                          {formatOperationLogTime(entry.timestamp)}
                        </time>
                        <strong>{entry.operation}</strong>
                      </div>
                      <div className="settings-log-entry-meta">
                        <span className={`status-chip status-chip-${level.tone}`}>
                          <level.Icon size={13} aria-hidden="true" />
                          {level.label}
                        </span>
                        <span>HTTP {entry.status}</span>
                        <span>{formatOperationLogDuration(entry.duration_ms)}</span>
                      </div>
                    </li>
                  );
                })}
              </ol>
            )}
          </>
        ) : null}
      </section>

      <section
        className="settings-section settings-reload-section"
        aria-labelledby="settings-reload-title"
      >
        <div className="settings-section-heading">
          <div>
            <h3 id="settings-reload-title">配置重载</h3>
            <p>从磁盘重新读取账户配置，并刷新当前账户状态。</p>
          </div>
          <RefreshCw size={20} aria-hidden="true" />
        </div>
        <button
          className="button button-primary settings-reload-button"
          disabled={reloadMutation.isPending}
          type="button"
          onClick={() => reloadMutation.mutate()}
        >
          <RefreshCw
            className={reloadMutation.isPending ? "button-spinner" : undefined}
            size={15}
            aria-hidden="true"
          />
          {reloadMutation.isPending ? "正在重载" : "重载配置"}
        </button>
        {reloadMutation.isError ? (
          <div className="settings-reload-error" role="alert">
            {getApiErrorMessage(reloadMutation.error)}
          </div>
        ) : null}
      </section>
    </section>
  );
}
