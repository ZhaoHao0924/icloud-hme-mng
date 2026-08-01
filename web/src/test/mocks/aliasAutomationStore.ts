import type { AliasAutomation } from "../../api/schemas";

export type MockAliasAutomationInput = Pick<
  AliasAutomation,
  | "enabled"
  | "interval_minutes"
  | "scheduled_batch_size"
  | "minimum_active"
  | "target_active"
  | "max_batch_size"
  | "label_prefix"
>;

const defaultAutomation: AliasAutomation = {
  enabled: false,
  interval_minutes: 60,
  label_prefix: "",
  last_active: 0,
  last_created: 0,
  last_error: "",
  last_run_at: "",
  last_status: "",
  max_batch_size: 5,
  minimum_active: 0,
  next_run_at: "",
  scheduled_batch_size: 0,
  target_active: 0,
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
      input: { active: number; created: number; status: AliasAutomation["last_status"] },
    ) {
      const current = read(accountId);
      const next: AliasAutomation = {
        ...current,
        last_active: input.active,
        last_created: input.created,
        last_error: "",
        last_run_at: "2026-08-01T09:00:00.000Z",
        last_status: input.status,
        next_run_at: current.enabled ? nextRunAt(current.interval_minutes) : "",
      };
      automationByAccount.set(accountId, next);
      return { ...next };
    },
    reset() {
      automationByAccount = new Map<string, AliasAutomation>();
    },
    update(accountId: string, input: MockAliasAutomationInput) {
      const current = read(accountId);
      const next: AliasAutomation = {
        ...current,
        ...input,
        label_prefix: input.label_prefix.trim(),
        next_run_at: input.enabled ? nextRunAt(input.interval_minutes) : "",
      };
      automationByAccount.set(accountId, next);
      return { ...next };
    },
  };
}

export type MockAliasAutomationStore = ReturnType<typeof createMockAliasAutomationStore>;
