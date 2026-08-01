import { CheckCircle2, Database, RefreshCw, Server, ShieldAlert } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, getApiErrorMessage } from "../../api/client";
import { healthQueryOptions, queryKeys } from "../../api/queries";
import { ErrorState } from "../../components/ErrorState";
import { LoadingState } from "../../components/LoadingState";
import { useNotifications } from "../../components/notificationContext";

function healthStatusMeta(status: "ok" | "degraded") {
  return status === "ok"
    ? { label: "正常", tone: "success" as const }
    : { label: "降级", tone: "warning" as const };
}

export function SettingsPage() {
  const queryClient = useQueryClient();
  const { notify } = useNotifications();
  const healthQuery = useQuery(healthQueryOptions());
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
