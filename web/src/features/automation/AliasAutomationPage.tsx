import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Eye, LoaderCircle, Pause, Play, Save } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";

import { api, getApiErrorMessage } from "../../api/client";
import { aliasAutomationQueryOptions, aliasesQueryOptions, queryKeys } from "../../api/queries";
import type { AliasAutomation, AliasAutomationPreview } from "../../api/schemas";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { LoadingState } from "../../components/LoadingState";
import { useNotifications } from "../../components/notificationContext";
import { useAccountDetailContext } from "../accounts/accountDetailContext";
import { AccountRequestErrorState } from "../security/SessionRecoveryView";
import { BatchCreateDialog } from "./BatchCreateDialog";
import { AliasCreationHistoryPanel } from "./AliasCreationHistoryPanel";
import { aliasAutomationFormSchema, type AliasAutomationFormValues } from "./aliasAutomationSchema";

const formDefaults: AliasAutomationFormValues = {
  enabled: false,
  intervalMinutes: 60,
  allowedWeekdays: [],
  executionWindowStart: "",
  executionWindowEnd: "",
  scheduledBatchSize: 0,
  minimumActive: 0,
  targetActive: 0,
  maxBatchSize: 5,
  maxTotalAliases: 1000,
  maxFailureCount: 3,
  dailyCreationLimit: 0,
  targetCreated: 0,
  labelPrefix: "",
};

function automationToFormValues(automation: AliasAutomation): AliasAutomationFormValues {
  return {
    enabled: automation.enabled,
    intervalMinutes: automation.interval_minutes,
    allowedWeekdays: automation.allowed_weekdays,
    executionWindowStart: automation.execution_window_start,
    executionWindowEnd: automation.execution_window_end,
    scheduledBatchSize: automation.scheduled_batch_size,
    minimumActive: automation.minimum_active,
    targetActive: automation.target_active,
    maxBatchSize: automation.max_batch_size,
    maxTotalAliases: automation.max_total_aliases,
    maxFailureCount: automation.max_failure_count,
    dailyCreationLimit: automation.daily_creation_limit,
    targetCreated: automation.target_created,
    labelPrefix: automation.label_prefix,
  };
}

const weekdayOptions = [
  { label: "周一", value: 1 },
  { label: "周二", value: 2 },
  { label: "周三", value: 3 },
  { label: "周四", value: 4 },
  { label: "周五", value: 5 },
  { label: "周六", value: 6 },
  { label: "周日", value: 0 },
] as const;

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

function pauseReasonLabel(reason: AliasAutomation["pause_reason"]) {
  switch (reason) {
    case "target_reached":
      return "累计创建目标已完成";
    case "alias_limit":
      return "已达到总别名安全上限";
    case "failure_limit":
      return "连续创建失败次数达到上限";
    case "manual":
      return "规则已手动暂停";
    default:
      return "";
  }
}

function activeAliasLabel(aliases: number | null) {
  return aliases === null ? "读取失败" : aliases === -1 ? "同步中" : `${aliases} 个`;
}

function executionDaysLabel(allowedWeekdays: number[]) {
  if (allowedWeekdays.length === 0) return "每天";
  return weekdayOptions
    .filter((weekday) => allowedWeekdays.includes(weekday.value))
    .map((weekday) => weekday.label)
    .join("、");
}

function executionWindowLabel(automation: AliasAutomation) {
  if (automation.execution_window_start === "" || automation.execution_window_end === "") {
    return "全天";
  }
  return `${automation.execution_window_start} - ${automation.execution_window_end}`;
}

export function AliasAutomationPage() {
  const { account } = useAccountDetailContext();
  const queryClient = useQueryClient();
  const { notify } = useNotifications();
  const [preview, setPreview] = useState<AliasAutomationPreview | null>(null);
  const [pendingTargetChange, setPendingTargetChange] = useState<AliasAutomationFormValues | null>(
    null,
  );
  const automationQuery = useQuery(aliasAutomationQueryOptions(account.id));
  const aliasesQuery = useQuery(aliasesQueryOptions(account.id));
  const {
    control,
    formState: { errors },
    handleSubmit,
    register,
    reset,
  } = useForm<AliasAutomationFormValues>({
    defaultValues: formDefaults,
    resolver: zodResolver(aliasAutomationFormSchema),
  });
  useEffect(() => {
    if (automationQuery.data) {
      reset(automationToFormValues(automationQuery.data));
    }
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
        allowedWeekdays: values.allowedWeekdays,
        executionWindowStart: values.executionWindowStart,
        executionWindowEnd: values.executionWindowEnd,
        scheduledBatchSize: values.scheduledBatchSize,
        minimumActive: values.minimumActive,
        targetActive: values.targetActive,
        maxBatchSize: values.maxBatchSize,
        maxTotalAliases: values.maxTotalAliases,
        maxFailureCount: values.maxFailureCount,
        dailyCreationLimit: values.dailyCreationLimit,
        targetCreated: values.targetCreated,
        labelPrefix: values.labelPrefix,
      }),
    onSuccess: async (automation) => {
      reset(automationToFormValues(automation));
      setPreview(null);
      notify({
        title: "自动化规则已保存",
        message:
          automation.target_created > 0 && automation.created_total >= automation.target_created
            ? "累计创建目标已完成"
            : automation.enabled
              ? "规则已启用"
              : automation.pause_reason
                ? `规则已暂停：${pauseReasonLabel(automation.pause_reason)}`
                : "规则已停用",
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
      setPreview(null);
      const dailyLimitReached =
        result.automation.daily_creation_limit > 0 &&
        result.automation.daily_created >= result.automation.daily_creation_limit &&
        result.automation.last_error !== "";
      notify({
        title: result.automation.pause_reason
          ? "自动化规则已自动暂停"
          : dailyLimitReached
            ? "已达到每日自动创建上限"
            : result.complete
              ? "自动化规则已执行"
              : "自动化规则部分完成",
        message: result.automation.pause_reason
          ? `${pauseReasonLabel(result.automation.pause_reason)}，已创建 ${result.created} 个别名`
          : dailyLimitReached
            ? result.created > 0
              ? `已创建 ${result.created} 个别名，将在次日继续`
              : "将在次日继续"
            : result.status === "skipped"
              ? result.automation.target_created > 0 &&
                result.automation.created_total >= result.automation.target_created
                ? "累计创建目标已完成"
                : "当前库存无需创建"
              : `已创建 ${result.created} 个别名`,
        tone:
          result.automation.pause_reason || dailyLimitReached || !result.complete
            ? "warning"
            : "success",
      });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.aliasAutomation(account.id) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.aliasCreationHistory(account.id) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.aliases(account.id) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.accounts }),
      ]);
    },
    retry: false,
  });

  const previewAutomation = useMutation({
    mutationFn: () => api.previewAliasAutomation(account.id),
    onSuccess: (result) => {
      setPreview(result);
    },
    retry: false,
  });

  const pauseAutomation = useMutation({
    mutationFn: () => api.pauseAliasAutomation(account.id),
    onSuccess: async (automation) => {
      reset(automationToFormValues(automation));
      setPreview(null);
      notify({
        title: "自动化规则已暂停",
        message: pauseReasonLabel(automation.pause_reason),
        tone: "warning",
      });
      await queryClient.invalidateQueries({ queryKey: queryKeys.aliasAutomation(account.id) });
    },
    retry: false,
  });

  const resumeAutomation = useMutation({
    mutationFn: () => api.resumeAliasAutomation(account.id),
    onSuccess: async (automation) => {
      reset(automationToFormValues(automation));
      setPreview(null);
      notify({
        title: "自动化规则已恢复",
        message: "规则已启用，并将按计划继续执行",
        tone: "success",
      });
      await queryClient.invalidateQueries({ queryKey: queryKeys.aliasAutomation(account.id) });
    },
    retry: false,
  });

  function save(values: AliasAutomationFormValues) {
    saveAutomation.reset();
    setPreview(null);
    saveAutomation.mutate(values);
  }

  async function confirmTargetChange() {
    if (!pendingTargetChange) return;
    saveAutomation.reset();
    await saveAutomation.mutateAsync(pendingTargetChange);
  }

  function submit(values: AliasAutomationFormValues) {
    if (saveAutomation.isPending) return;
    if (values.targetCreated !== automation.target_created) {
      setPendingTargetChange(values);
      return;
    }
    save(values);
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
  const pending =
    saveAutomation.isPending ||
    runAutomation.isPending ||
    previewAutomation.isPending ||
    pauseAutomation.isPending ||
    resumeAutomation.isPending;
  const targetCompleted =
    automation.target_created > 0 && automation.created_total >= automation.target_created;
  const paused = automation.pause_reason !== "" && !automation.enabled;
  const ruleState = paused
    ? `已暂停：${pauseReasonLabel(automation.pause_reason)}`
    : automation.enabled
      ? "运行中"
      : "已停用";
  const mutationError = saveAutomation.isError
    ? saveAutomation.error
    : runAutomation.isError
      ? runAutomation.error
      : pauseAutomation.isError
        ? pauseAutomation.error
        : resumeAutomation.isError
          ? resumeAutomation.error
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
          <dt>创建进度</dt>
          <dd>
            {automation.target_created > 0
              ? `${automation.created_total} / ${automation.target_created}`
              : "未设置"}
          </dd>
        </div>
        <div className="automation-status-item">
          <dt>今日自动创建</dt>
          <dd>
            {automation.daily_creation_limit > 0
              ? `${automation.daily_created} / ${automation.daily_creation_limit}`
              : "未限制"}
          </dd>
        </div>
        <div className="automation-status-item">
          <dt>执行日</dt>
          <dd>{executionDaysLabel(automation.allowed_weekdays)}</dd>
        </div>
        <div className="automation-status-item">
          <dt>执行时间</dt>
          <dd>{executionWindowLabel(automation)}</dd>
        </div>
        <div className="automation-status-item">
          <dt>规则状态</dt>
          <dd>{ruleState}</dd>
        </div>
        <div className="automation-status-item">
          <dt>总别名安全上限</dt>
          <dd>{automation.max_total_aliases} 个</dd>
        </div>
        <div className="automation-status-item">
          <dt>连续失败</dt>
          <dd>
            {automation.consecutive_failure} / {automation.max_failure_count}
          </dd>
        </div>
        <div className="automation-status-item">
          <dt>下次执行</dt>
          <dd>
            {targetCompleted
              ? "目标已完成"
              : paused
                ? "等待手动恢复"
                : automation.enabled
                  ? formatTime(automation.next_run_at)
                  : "已停用"}
          </dd>
        </div>
      </dl>

      <form
        className="automation-form"
        noValidate
        onInput={() => setPreview(null)}
        onSubmit={(event) => void handleSubmit(submit)(event)}
      >
        <div className="automation-switch-row">
          <div>
            <label htmlFor="automation-enabled">启用自动化规则</label>
            <span>
              {targetCompleted
                ? "目标已完成"
                : paused
                  ? `${pauseReasonLabel(automation.pause_reason)}；恢复后继续执行`
                  : automation.enabled
                    ? "规则已启用"
                    : "规则已停用"}
            </span>
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
          <Controller
            control={control}
            name="allowedWeekdays"
            render={({ field }) => {
              const selectedWeekdays = field.value ?? [];
              return (
                <fieldset className="automation-weekday-field">
                  <legend>执行日</legend>
                  <div className="automation-weekday-options">
                    {weekdayOptions.map((weekday) => (
                      <label key={weekday.value} className="automation-weekday-option">
                        <input
                          checked={selectedWeekdays.includes(weekday.value)}
                          disabled={pending}
                          type="checkbox"
                          onChange={() => {
                            const next = selectedWeekdays.includes(weekday.value)
                              ? selectedWeekdays.filter((value) => value !== weekday.value)
                              : [...selectedWeekdays, weekday.value];
                            field.onChange(next.sort((left, right) => left - right));
                            setPreview(null);
                          }}
                        />
                        <span>{weekday.label}</span>
                      </label>
                    ))}
                  </div>
                  {errors.allowedWeekdays ? (
                    <span className="field-error">{errors.allowedWeekdays.message}</span>
                  ) : null}
                </fieldset>
              );
            }}
          />

          <div className="form-field automation-window-field">
            <label>执行时间</label>
            <div className="automation-window-inputs">
              <div>
                <label htmlFor="automation-window-start">开始</label>
                <input
                  id="automation-window-start"
                  aria-describedby={
                    errors.executionWindowStart ? "automation-window-start-error" : undefined
                  }
                  aria-invalid={Boolean(errors.executionWindowStart)}
                  disabled={pending}
                  type="time"
                  {...register("executionWindowStart")}
                />
              </div>
              <div>
                <label htmlFor="automation-window-end">结束</label>
                <input
                  id="automation-window-end"
                  aria-describedby={
                    errors.executionWindowEnd ? "automation-window-end-error" : undefined
                  }
                  aria-invalid={Boolean(errors.executionWindowEnd)}
                  disabled={pending}
                  type="time"
                  {...register("executionWindowEnd")}
                />
              </div>
            </div>
            {errors.executionWindowStart ? (
              <span className="field-error" id="automation-window-start-error">
                {errors.executionWindowStart.message}
              </span>
            ) : null}
            {errors.executionWindowEnd ? (
              <span className="field-error" id="automation-window-end-error">
                {errors.executionWindowEnd.message}
              </span>
            ) : null}
          </div>

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
            <label htmlFor="automation-target-created">累计创建目标</label>
            <input
              id="automation-target-created"
              aria-describedby={
                errors.targetCreated ? "automation-target-created-error" : undefined
              }
              aria-invalid={Boolean(errors.targetCreated)}
              disabled={pending}
              min={0}
              max={1000}
              type="number"
              {...register("targetCreated", { valueAsNumber: true })}
            />
            {errors.targetCreated ? (
              <span className="field-error" id="automation-target-created-error">
                {errors.targetCreated.message}
              </span>
            ) : null}
          </div>

          <div className="form-field">
            <label htmlFor="automation-daily-creation-limit">每日自动创建上限</label>
            <input
              id="automation-daily-creation-limit"
              aria-describedby={
                errors.dailyCreationLimit ? "automation-daily-creation-limit-error" : undefined
              }
              aria-invalid={Boolean(errors.dailyCreationLimit)}
              disabled={pending}
              min={0}
              max={1000}
              type="number"
              {...register("dailyCreationLimit", { valueAsNumber: true })}
            />
            {errors.dailyCreationLimit ? (
              <span className="field-error" id="automation-daily-creation-limit-error">
                {errors.dailyCreationLimit.message}
              </span>
            ) : null}
          </div>

          <div className="form-field">
            <label htmlFor="automation-max-total-aliases">总别名安全上限</label>
            <input
              id="automation-max-total-aliases"
              aria-describedby={
                errors.maxTotalAliases ? "automation-max-total-aliases-error" : undefined
              }
              aria-invalid={Boolean(errors.maxTotalAliases)}
              disabled={pending}
              min={1}
              max={1000}
              type="number"
              {...register("maxTotalAliases", { valueAsNumber: true })}
            />
            {errors.maxTotalAliases ? (
              <span className="field-error" id="automation-max-total-aliases-error">
                {errors.maxTotalAliases.message}
              </span>
            ) : null}
          </div>

          <div className="form-field">
            <label htmlFor="automation-max-failure-count">连续失败上限</label>
            <input
              id="automation-max-failure-count"
              aria-describedby={
                errors.maxFailureCount ? "automation-max-failure-count-error" : undefined
              }
              aria-invalid={Boolean(errors.maxFailureCount)}
              disabled={pending}
              min={1}
              max={10}
              type="number"
              {...register("maxFailureCount", { valueAsNumber: true })}
            />
            {errors.maxFailureCount ? (
              <span className="field-error" id="automation-max-failure-count-error">
                {errors.maxFailureCount.message}
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
        {previewAutomation.isError ? (
          <div className="form-submit-error" role="alert">
            {getApiErrorMessage(previewAutomation.error)}
          </div>
        ) : null}
        {preview ? (
          <section className="automation-preview" aria-label="执行预览" aria-live="polite">
            <div className="automation-preview-heading">
              <strong>执行预览</strong>
              <span>{preview.schedule_allowed ? "当前计划允许" : preview.schedule_reason}</span>
            </div>
            <dl className="automation-preview-grid">
              <div>
                <dt>本次可创建</dt>
                <dd>{preview.requested} 个</dd>
              </div>
              <div>
                <dt>别名数量</dt>
                <dd>
                  {preview.active_aliases} / {preview.total_aliases}
                </dd>
              </div>
              <div>
                <dt>每日余量</dt>
                <dd>
                  {preview.automation.daily_creation_limit > 0
                    ? `${preview.daily_remaining} 个`
                    : "未限制"}
                </dd>
              </div>
              <div>
                <dt>总上限余量</dt>
                <dd>
                  {preview.remaining_total_capacity} / {preview.max_total_aliases}
                </dd>
              </div>
              <div>
                <dt>累计目标余量</dt>
                <dd>
                  {preview.automation.target_created > 0
                    ? `${preview.target_remaining} 个`
                    : "未设置"}
                </dd>
              </div>
              <div>
                <dt>下个允许时间</dt>
                <dd>
                  {preview.schedule_allowed
                    ? "当前"
                    : preview.next_eligible_at
                      ? formatTime(preview.next_eligible_at)
                      : "未计算"}
                </dd>
              </div>
            </dl>
          </section>
        ) : null}

        <div className="automation-actions">
          {automation.enabled ? (
            <button
              className="button button-secondary"
              type="button"
              disabled={pending}
              onClick={() => {
                pauseAutomation.reset();
                pauseAutomation.mutate();
              }}
            >
              {pauseAutomation.isPending ? (
                <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
              ) : (
                <Pause size={16} aria-hidden="true" />
              )}
              {pauseAutomation.isPending ? "正在暂停" : "暂停规则"}
            </button>
          ) : paused && !targetCompleted ? (
            <button
              className="button button-secondary"
              type="button"
              disabled={pending}
              onClick={() => {
                resumeAutomation.reset();
                resumeAutomation.mutate();
              }}
            >
              {resumeAutomation.isPending ? (
                <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
              ) : (
                <Play size={16} aria-hidden="true" />
              )}
              {resumeAutomation.isPending ? "正在恢复" : "恢复规则"}
            </button>
          ) : null}
          <button
            className="button button-secondary"
            type="button"
            disabled={pending}
            onClick={() => {
              previewAutomation.reset();
              previewAutomation.mutate();
            }}
          >
            {previewAutomation.isPending ? (
              <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
            ) : (
              <Eye size={16} aria-hidden="true" />
            )}
            {previewAutomation.isPending ? "正在预览" : "预览执行"}
          </button>
          <button
            className="button button-secondary"
            type="button"
            disabled={pending || paused || targetCompleted}
            title={
              paused
                ? targetCompleted
                  ? "修改累计创建目标并保存后才能再次执行"
                  : "恢复规则后才能再次执行"
                : undefined
            }
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

      <ConfirmDialog
        cancelLabel="返回修改"
        confirmLabel="确认重置"
        description={
          pendingTargetChange?.targetCreated === 0
            ? "关闭累计创建目标会清零当前创建进度，是否继续？"
            : `将累计创建目标调整为 ${pendingTargetChange?.targetCreated ?? 0} 会清零当前创建进度，并从头计数，是否继续？`
        }
        onConfirm={confirmTargetChange}
        onOpenChange={(open) => {
          if (!open) setPendingTargetChange(null);
        }}
        open={pendingTargetChange !== null}
        pending={saveAutomation.isPending}
        title="确认重置累计创建进度"
      />

      <AliasCreationHistoryPanel accountId={account.id} />
    </section>
  );
}
