import type { AliasAutomation } from "../../api/schemas";

export type MockAliasAutomationInput = Pick<
  AliasAutomation,
  | "enabled"
  | "interval_minutes"
  | "scheduled_batch_size"
  | "minimum_active"
  | "target_active"
  | "max_batch_size"
  | "max_total_aliases"
  | "max_failure_count"
  | "daily_creation_limit"
  | "target_created"
  | "label_prefix"
> &
  Partial<
    Pick<AliasAutomation, "allowed_weekdays" | "execution_window_start" | "execution_window_end">
  >;

const defaultAutomation: AliasAutomation = {
  enabled: false,
  interval_minutes: 60,
  allowed_weekdays: [],
  execution_window_start: "",
  execution_window_end: "",
  label_prefix: "",
  last_active: 0,
  last_created: 0,
  last_error: "",
  last_run_at: "",
  last_status: "",
  max_batch_size: 5,
  max_total_aliases: 1000,
  max_failure_count: 3,
  daily_creation_limit: 0,
  daily_created: 0,
  daily_created_date: "",
  minimum_active: 0,
  next_run_at: "",
  consecutive_failure: 0,
  pause_reason: "",
  scheduled_batch_size: 0,
  target_active: 0,
  target_created: 0,
  created_total: 0,
};

function nextRunAt(intervalMinutes: number) {
  return new Date(Date.UTC(2026, 7, 1, 9, intervalMinutes, 0)).toISOString();
}

export function createMockAliasAutomationStore() {
  let automationByAccount = new Map<string, AliasAutomation>();

  function read(accountId: string) {
    return { ...(automationByAccount.get(accountId) ?? defaultAutomation) };
  }

  return {
    get(accountId: string) {
      return read(accountId);
    },
    recordRun(
      accountId: string,
      input: {
        active: number;
        created: number;
        error?: string;
        nextRunAt?: string;
        status: AliasAutomation["last_status"];
      },
    ) {
      const current = read(accountId);
      const createdTotal =
        current.target_created > 0
          ? Math.min(current.target_created, current.created_total + input.created)
          : 0;
      const targetCompleted = current.target_created > 0 && createdTotal >= current.target_created;
      const pauseReason = targetCompleted ? "target_reached" : "";
      const dailyCreated =
        current.daily_creation_limit > 0
          ? Math.min(current.daily_creation_limit, current.daily_created + input.created)
          : 0;
      const next: AliasAutomation = {
        ...current,
        created_total: createdTotal,
        daily_created: dailyCreated,
        daily_created_date: current.daily_creation_limit > 0 ? "2026-08-02" : "",
        enabled: targetCompleted ? false : current.enabled,
        last_active: input.active,
        last_created: input.created,
        last_error: input.error ?? "",
        last_run_at: "2026-08-01T09:00:00.000Z",
        last_status: input.status,
        consecutive_failure: input.status === "success" ? 0 : current.consecutive_failure,
        pause_reason: pauseReason,
        next_run_at:
          targetCompleted || !current.enabled
            ? ""
            : (input.nextRunAt ?? nextRunAt(current.interval_minutes)),
      };
      automationByAccount.set(accountId, next);
      return { ...next };
    },
    pause(accountId: string) {
      const current = read(accountId);
      const targetCompleted =
        current.target_created > 0 && current.created_total >= current.target_created;
      const next: AliasAutomation = {
        ...current,
        enabled: false,
        next_run_at: "",
        pause_reason: targetCompleted ? "target_reached" : "manual",
      };
      automationByAccount.set(accountId, next);
      return { ...next };
    },
    reset() {
      automationByAccount = new Map<string, AliasAutomation>();
    },
    resume(accountId: string) {
      const current = read(accountId);
      if (current.target_created > 0 && current.created_total >= current.target_created) {
        return null;
      }
      const next: AliasAutomation = {
        ...current,
        consecutive_failure: 0,
        enabled: true,
        next_run_at: nextRunAt(current.interval_minutes),
        pause_reason: "",
      };
      automationByAccount.set(accountId, next);
      return { ...next };
    },
    update(accountId: string, input: MockAliasAutomationInput) {
      const current = read(accountId);
      const createdTotal =
        input.target_created === current.target_created ? current.created_total : 0;
      const targetCompleted = input.target_created > 0 && createdTotal >= input.target_created;
      const enabled = targetCompleted ? false : input.enabled;
      const dailyCreated =
        input.daily_creation_limit > 0 &&
        input.daily_creation_limit === current.daily_creation_limit
          ? Math.min(input.daily_creation_limit, current.daily_created)
          : 0;
      const next: AliasAutomation = {
        ...current,
        ...input,
        allowed_weekdays: [...(input.allowed_weekdays ?? current.allowed_weekdays)],
        execution_window_start: input.execution_window_start ?? current.execution_window_start,
        execution_window_end: input.execution_window_end ?? current.execution_window_end,
        created_total: createdTotal,
        daily_created: dailyCreated,
        daily_created_date: input.daily_creation_limit > 0 ? "2026-08-02" : "",
        consecutive_failure: input.enabled ? 0 : current.consecutive_failure,
        enabled,
        label_prefix: input.label_prefix.trim(),
        next_run_at: enabled ? nextRunAt(input.interval_minutes) : "",
        pause_reason: targetCompleted
          ? "target_reached"
          : input.enabled
            ? ""
            : current.pause_reason,
      };
      automationByAccount.set(accountId, next);
      return { ...next };
    },
  };
}

export type MockAliasAutomationStore = ReturnType<typeof createMockAliasAutomationStore>;
