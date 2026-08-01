import type { Account, AccountStatus } from "../../api/schemas";

export type AccountStatusTone = "danger" | "success" | "warning";

const statusMeta: Record<AccountStatus, { label: string; tone: AccountStatusTone }> = {
  active: { label: "正常", tone: "success" },
  error: { label: "异常", tone: "danger" },
  pending: { label: "待登录", tone: "warning" },
};

export function accountStatusMeta(status: AccountStatus) {
  return statusMeta[status];
}

export function accountStatusSummary(accounts: Account[]) {
  return accounts.reduce(
    (summary, account) => {
      summary.total += 1;
      if (account.status === "active") summary.active += 1;
      if (account.status === "pending") summary.pending += 1;
      if (account.status === "error") summary.error += 1;
      return summary;
    },
    { active: 0, error: 0, pending: 0, total: 0 },
  );
}

export function formatAccountDate(value: string) {
  if (!value) return "未校验";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "未校验";
  return new Intl.DateTimeFormat("zh-CN", {
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    month: "2-digit",
    year: "numeric",
  }).format(date);
}
