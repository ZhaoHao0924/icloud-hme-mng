import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LoaderCircle, Play, Save } from "lucide-react";
import { useEffect, useMemo } from "react";
import { useForm } from "react-hook-form";

import { api, getApiErrorMessage } from "../../api/client";
import { aliasAutomationQueryOptions, aliasesQueryOptions, queryKeys } from "../../api/queries";
import type { AliasAutomation } from "../../api/schemas";
import { LoadingState } from "../../components/LoadingState";
import { useNotifications } from "../../components/notificationContext";
import { useAccountDetailContext } from "../accounts/accountDetailContext";
import { AccountRequestErrorState } from "../security/SessionRecoveryView";
import { BatchCreateDialog } from "./BatchCreateDialog";
import { aliasAutomationFormSchema, type AliasAutomationFormValues } from "./aliasAutomationSchema";

const formDefaults: AliasAutomationFormValues = {
  enabled: false,
  intervalMinutes: 60,
  scheduledBatchSize: 0,
  minimumActive: 0,
  targetActive: 0,
  maxBatchSize: 5,
  labelPrefix: "",
};

function automationToFormValues(automation: AliasAutomation): AliasAutomationFormValues {
  return {
    enabled: automation.enabled,
    intervalMinutes: automation.interval_minutes,
    scheduledBatchSize: automation.scheduled_batch_size,
    minimumActive: automation.minimum_active,
    targetActive: automation.target_active,
    maxBatchSize: automation.max_batch_size,
    labelPrefix: automation.label_prefix,
  };
}

function formatTime(value: string) {
  if (!value) return "未执行";
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

function statusLabel(status: AliasAutomation["last_status"]) {
  switch (status) {
    case "success":
      return "完成";
    case "partial":
      return "部分完成";
    case "skipped":
      return "无需创建";
    case "error":
      return "失败";
    default:
      return "未执行";
  }
}

function activeAliasLabel(aliases: number | null) {
  return aliases === null ? "读取失败" : aliases === -1 ? "同步中" : `${aliases} 个`;
}

export function AliasAutomationPage() {
  const { account } = useAccountDetailContext();
  const queryClient = useQueryClient();
  const { notify } = useNotifications();
  const automationQuery = useQuery(aliasAutomationQueryOptions(account.id));
  const aliasesQuery = useQuery(aliasesQueryOptions(account.id));
  const {
    formState: { errors },
    handleSubmit,
    register,
    reset,
  } = useForm<AliasAutomationFormValues>({
    defaultValues: formDefaults,
    resolver: zodResolver(aliasAutomationFormSchema),
  });

  useEffect(() => {
    if (automationQuery.data) reset(automationToFormValues(automationQuery.data));
  }, [automationQuery.data, reset]);

  const activeAliases = useMemo(() => {
    if (aliasesQuery.isError) return null;
    if (!aliasesQuery.data) return -1;
    return aliasesQuery.data.aliases.filter((alias) => alias.active).length;
  }, [aliasesQuery.data, aliasesQuery.isError]);

  const saveAutomation = useMutation({
    mutationFn: (values: AliasAutomationFormValues) =>
      api.updateAliasAutomation(account.id, {
        enabled: values.enabled,
        intervalMinutes: values.intervalMinutes,
        scheduledBatchSize: values.scheduledBatchSize,
        minimumActive: values.minimumActive,
        targetActive: values.targetActive,
        maxBatchSize: values.maxBatchSize,
        labelPrefix: values.labelPrefix,
      }),
    onSuccess: async (automation) => {
      reset(automationToFormValues(automation));
      notify({
        title: "自动化规则已保存",
        message: automation.enabled ? "规则已启用" : "规则已停用",
        tone: "success",
      });
      await queryClient.invalidateQueries({ queryKey: queryKeys.aliasAutomation(account.id) });
    },
    retry: false,
  });

  const runAutomation = useMutation({
    mutationFn: () => api.runAliasAutomation(account.id),
    onSuccess: async (result) => {
      reset(automationToFormValues(result.automation));
      notify({
        title: result.complete ? "自动化规则已执行" : "自动化规则部分完成",
        message:
          result.status === "skipped" ? "当前库存无需创建" : `已创建 ${result.created} 个别名`,
        tone: result.complete ? "success" : "warning",
      });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.aliasAutomation(account.id) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.aliases(account.id) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.accounts }),
      ]);
    },
    retry: false,
  });

  function submit(values: AliasAutomationFormValues) {
    if (saveAutomation.isPending) return;
    saveAutomation.reset();
    saveAutomation.mutate(values);
  }

  if (automationQuery.isPending) {
    return <LoadingState label="正在读取自动化规则" />;
  }

  if (automationQuery.isError) {
    return (
      <AccountRequestErrorState
        accountId={account.id}
        error={automationQuery.error}
        onRetry={() => void automationQuery.refetch()}
        title="自动化规则加载失败"
      />
    );
  }

  const automation = automationQuery.data;
  const pending = saveAutomation.isPending || runAutomation.isPending;
  const mutationError = saveAutomation.isError
    ? saveAutomation.error
    : runAutomation.isError
      ? runAutomation.error
      : null;

  return (
    <section className="automation-page" aria-labelledby="automation-title">
      <div className="section-heading">
        <div>
          <h3 id="automation-title">别名自动化</h3>
          <span className="record-count">账户级规则</span>
        </div>
        <BatchCreateDialog accountId={account.id} defaultLabelPrefix={automation.label_prefix} />
      </div>

      <dl className="automation-status-grid" aria-label="自动化运行状态">
        <div className="automation-status-item">
          <dt>活跃别名</dt>
          <dd>{activeAliasLabel(activeAliases)}</dd>
        </div>
        <div className="automation-status-item">
          <dt>上次结果</dt>
          <dd>{statusLabel(automation.last_status)}</dd>
        </div>
        <div className="automation-status-item">
          <dt>上次创建</dt>
          <dd>{automation.last_created} 个</dd>
        </div>
        <div className="automation-status-item">
          <dt>下次执行</dt>
          <dd>{automation.enabled ? formatTime(automation.next_run_at) : "已停用"}</dd>
        </div>
      </dl>

      <form
        className="automation-form"
        noValidate
        onSubmit={(event) => void handleSubmit(submit)(event)}
      >
        <div className="automation-switch-row">
          <div>
            <label htmlFor="automation-enabled">启用自动化规则</label>
            <span>{automation.enabled ? "规则已启用" : "规则已停用"}</span>
          </div>
          <input
            id="automation-enabled"
            className="automation-switch"
            type="checkbox"
            disabled={pending}
            {...register("enabled")}
          />
        </div>

        <div className="automation-form-grid">
          <div className="form-field">
            <label htmlFor="automation-interval">执行间隔（分钟）</label>
            <input
              id="automation-interval"
              aria-describedby={errors.intervalMinutes ? "automation-interval-error" : undefined}
              aria-invalid={Boolean(errors.intervalMinutes)}
              disabled={pending}
              min={5}
              max={10080}
              type="number"
              {...register("intervalMinutes", { valueAsNumber: true })}
            />
            {errors.intervalMinutes ? (
              <span className="field-error" id="automation-interval-error">
                {errors.intervalMinutes.message}
              </span>
            ) : null}
          </div>

          <div className="form-field">
            <label htmlFor="automation-scheduled-count">定时创建数量</label>
            <input
              id="automation-scheduled-count"
              aria-describedby={
                errors.scheduledBatchSize ? "automation-scheduled-count-error" : undefined
              }
              aria-invalid={Boolean(errors.scheduledBatchSize)}
              disabled={pending}
              min={0}
              max={20}
              type="number"
              {...register("scheduledBatchSize", { valueAsNumber: true })}
            />
            {errors.scheduledBatchSize ? (
              <span className="field-error" id="automation-scheduled-count-error">
                {errors.scheduledBatchSize.message}
              </span>
            ) : null}
          </div>

          <div className="form-field">
            <label htmlFor="automation-minimum-active">库存阈值</label>
            <input
              id="automation-minimum-active"
              aria-describedby={
                errors.minimumActive ? "automation-minimum-active-error" : undefined
              }
              aria-invalid={Boolean(errors.minimumActive)}
              disabled={pending}
              min={0}
              max={100}
              type="number"
              {...register("minimumActive", { valueAsNumber: true })}
            />
            {errors.minimumActive ? (
              <span className="field-error" id="automation-minimum-active-error">
                {errors.minimumActive.message}
              </span>
            ) : null}
          </div>

          <div className="form-field">
            <label htmlFor="automation-target-active">补充目标</label>
            <input
              id="automation-target-active"
              aria-describedby={errors.targetActive ? "automation-target-active-error" : undefined}
              aria-invalid={Boolean(errors.targetActive)}
              disabled={pending}
              min={0}
              max={100}
              type="number"
              {...register("targetActive", { valueAsNumber: true })}
            />
            {errors.targetActive ? (
              <span className="field-error" id="automation-target-active-error">
                {errors.targetActive.message}
              </span>
            ) : null}
          </div>

          <div className="form-field">
            <label htmlFor="automation-max-batch">单次上限</label>
            <input
              id="automation-max-batch"
              aria-describedby={errors.maxBatchSize ? "automation-max-batch-error" : undefined}
              aria-invalid={Boolean(errors.maxBatchSize)}
              disabled={pending}
              min={1}
              max={20}
              type="number"
              {...register("maxBatchSize", { valueAsNumber: true })}
            />
            {errors.maxBatchSize ? (
              <span className="field-error" id="automation-max-batch-error">
                {errors.maxBatchSize.message}
              </span>
            ) : null}
          </div>

          <div className="form-field automation-prefix-field">
            <label htmlFor="automation-label-prefix">标签前缀</label>
            <input
              id="automation-label-prefix"
              autoComplete="off"
              aria-describedby={errors.labelPrefix ? "automation-label-prefix-error" : undefined}
              aria-invalid={Boolean(errors.labelPrefix)}
              disabled={pending}
              maxLength={196}
              placeholder="例如：自动补充"
              {...register("labelPrefix")}
            />
            {errors.labelPrefix ? (
              <span className="field-error" id="automation-label-prefix-error">
                {errors.labelPrefix.message}
              </span>
            ) : null}
          </div>
        </div>

        {automation.last_error ? (
          <div className="form-submit-notice">{automation.last_error}</div>
        ) : null}
        {mutationError ? (
          <div className="form-submit-error" role="alert">
            {getApiErrorMessage(mutationError)}
          </div>
        ) : null}

        <div className="automation-actions">
          <button
            className="button button-secondary"
            type="button"
            disabled={pending}
            onClick={() => {
              runAutomation.reset();
              runAutomation.mutate();
            }}
          >
            {runAutomation.isPending ? (
              <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
            ) : (
              <Play size={16} aria-hidden="true" />
            )}
            {runAutomation.isPending ? "正在执行" : "立即执行规则"}
          </button>
          <button className="button button-primary" type="submit" disabled={pending}>
            {saveAutomation.isPending ? (
              <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
            ) : (
              <Save size={16} aria-hidden="true" />
            )}
            {saveAutomation.isPending ? "正在保存" : "保存规则"}
          </button>
        </div>
      </form>
    </section>
  );
}
