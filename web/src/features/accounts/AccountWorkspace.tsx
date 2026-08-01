import { CheckCircle2, Clock3, Database, KeyRound, ShieldAlert, Trash2, Users } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { Link } from "react-router-dom";

import { api, getApiErrorMessage } from "../../api/client";
import { accountsQueryOptions, queryKeys } from "../../api/queries";
import type { Account } from "../../api/schemas";
import { EmptyState } from "../../components/EmptyState";
import { ErrorState } from "../../components/ErrorState";
import { LoadingState } from "../../components/LoadingState";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { useNotifications } from "../../components/notificationContext";
import { AddAccountDialog } from "./AddAccountDialog";
import { accountStatusMeta, accountStatusSummary, formatAccountDate } from "./accountPresentation";
import { SessionRecoveryLink } from "../security/SessionRecoveryView";
import { isStoredSessionExpiredError } from "../security/sessionRecoveryState";

const columns = ["名称", "邮箱", "状态", "别名", "认证", "最后校验"];

function AccountStatusBand({
  accounts,
  loading = false,
}: {
  accounts: Account[];
  loading?: boolean;
}) {
  const summary = accountStatusSummary(accounts);
  const items = [
    { icon: Users, label: "账户总数", value: summary.total },
    { icon: CheckCircle2, label: "正常", value: summary.active },
    { icon: Clock3, label: "待登录", value: summary.pending },
    { icon: ShieldAlert, label: "异常", value: summary.error },
  ];

  return (
    <dl className="account-status-band" aria-label="账户状态汇总">
      {items.map(({ icon: Icon, label, value }) => (
        <div className="account-status-item" key={label}>
          <dt>
            <Icon size={16} aria-hidden="true" />
            <span>{label}</span>
          </dt>
          <dd>{loading ? "—" : value}</dd>
        </div>
      ))}
    </dl>
  );
}

function AccountStatus({
  accountId,
  status,
  error,
}: {
  accountId: string;
  status: Parameters<typeof accountStatusMeta>[0];
  error: string;
}) {
  const meta = accountStatusMeta(status);
  const sessionExpired = status === "error" && isStoredSessionExpiredError(error);
  return (
    <div className="account-status-cell">
      <span className={`status-chip status-chip-${meta.tone}`}>
        <span className="status-chip-dot" aria-hidden="true" />
        {meta.label}
      </span>
      {status === "error" && error ? (
        <span className="account-error" title={error}>
          {error}
        </span>
      ) : null}
      {sessionExpired ? (
        <SessionRecoveryLink accountId={accountId} className="account-recovery-link" />
      ) : status !== "active" ? (
        <Link
          className="account-recovery-link"
          to={`/accounts/${encodeURIComponent(accountId)}/security`}
        >
          <KeyRound size={13} aria-hidden="true" />
          {status === "error" ? "更新凭据" : "设置凭据"}
        </Link>
      ) : null}
    </div>
  );
}

function CapabilityTag({ configured, label }: { configured: boolean; label: string }) {
  return (
    <span
      className={`capability-tag ${configured ? "capability-configured" : "capability-missing"}`}
    >
      <span className="capability-dot" aria-hidden="true" />
      {label} {configured ? "已配置" : "未配置"}
    </span>
  );
}

function AccountTable({ accounts }: { accounts: Account[] }) {
  const queryClient = useQueryClient();
  const { notify } = useNotifications();
  const [accountToDelete, setAccountToDelete] = useState<Account | null>(null);
  const deleteButtonRef = useRef<HTMLButtonElement>(null);
  const deleteAccount = useMutation({
    mutationFn: (account: Account) => api.deleteAccount(account.id),
    onSuccess: async (_, account) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.accounts });
      notify({ title: "账户已删除", message: account.name, tone: "success" });
    },
  });

  return (
    <>
      <div className="table-frame">
        <table className="account-table">
          <caption className="visually-hidden">账户列表</caption>
          <thead>
            <tr>
              {columns.map((column) => (
                <th key={column} scope="col">
                  {column}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {accounts.map((account) => (
              <tr key={account.id}>
                <th scope="row" data-label="名称">
                  <div className="account-name-row">
                    <span>
                      <Link
                        className="account-name account-name-link"
                        to={`/accounts/${encodeURIComponent(account.id)}/aliases`}
                        aria-label={`打开账户 ${account.name}`}
                      >
                        {account.name}
                      </Link>
                      <span className="account-id">{account.id}</span>
                    </span>
                    <button
                      className="icon-button account-delete-button"
                      type="button"
                      aria-label={`删除账户 ${account.name}`}
                      title={`删除账户 ${account.name}`}
                      onClick={(event) => {
                        deleteButtonRef.current = event.currentTarget;
                        setAccountToDelete(account);
                      }}
                    >
                      <Trash2 size={16} aria-hidden="true" />
                    </button>
                  </div>
                </th>
                <td data-label="邮箱">
                  <span className="account-email">{account.icloud_email || "未设置"}</span>
                  {account.real_email ? (
                    <span className="account-secondary">{account.real_email}</span>
                  ) : null}
                </td>
                <td data-label="状态">
                  <AccountStatus
                    accountId={account.id}
                    status={account.status}
                    error={account.last_error}
                  />
                </td>
                <td data-label="别名">
                  <span className="account-alias-count">{account.alias_active}</span>
                  <span className="account-secondary">/ {account.alias_total} 使用中</span>
                </td>
                <td data-label="认证">
                  <div className="capability-list">
                    <CapabilityTag configured={account.has_cookies} label="Cookie" />
                    <CapabilityTag configured={account.has_app_password} label="App 密码" />
                  </div>
                </td>
                <td data-label="最后校验">{formatAccountDate(account.last_validated)}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {accounts.length === 0 ? (
          <EmptyState
            action={<AddAccountDialog />}
            icon={<Database size={24} strokeWidth={1.75} />}
            title="暂无账户"
          />
        ) : null}
      </div>
      <ConfirmDialog
        confirmLabel="删除账户"
        description={`删除“${accountToDelete?.name ?? ""}”后，本地配置和登录状态都会移除，且无法恢复。`}
        destructive
        onConfirm={() => {
          if (!accountToDelete) return Promise.resolve();
          return deleteAccount.mutateAsync(accountToDelete).then(() => undefined);
        }}
        onConfirmError={(error) => {
          notify({ message: getApiErrorMessage(error), title: "删除失败", tone: "error" });
        }}
        onOpenChange={(open) => {
          if (!open && !deleteAccount.isPending) {
            setAccountToDelete(null);
            deleteAccount.reset();
          }
        }}
        open={accountToDelete !== null}
        pending={deleteAccount.isPending}
        returnFocusRef={deleteButtonRef}
        title="确认删除账户？"
      />
    </>
  );
}

export function AccountWorkspace() {
  const accountsQuery = useQuery(accountsQueryOptions());
  const accounts = accountsQuery.data ?? [];
  const countLabel = accountsQuery.isPending
    ? "加载中"
    : accountsQuery.isError
      ? "加载失败"
      : `${accounts.length} 个账户`;

  return (
    <section className="account-list" aria-labelledby="account-list-title">
      <div className="section-heading">
        <div>
          <h2 id="account-list-title">所有账户</h2>
          <span className="record-count">{countLabel}</span>
        </div>
        {!accountsQuery.isSuccess || accounts.length > 0 ? <AddAccountDialog /> : null}
      </div>

      <AccountStatusBand
        accounts={accounts}
        loading={accountsQuery.isPending || accountsQuery.isError}
      />

      {accountsQuery.isPending ? (
        <div className="table-frame account-table-state">
          <LoadingState label="正在读取账户" />
        </div>
      ) : accountsQuery.isError ? (
        <ErrorState
          action={
            <button
              className="button button-secondary"
              type="button"
              onClick={() => void accountsQuery.refetch()}
            >
              重新加载
            </button>
          }
          description={getApiErrorMessage(accountsQuery.error)}
        />
      ) : (
        <AccountTable accounts={accounts} />
      )}
    </section>
  );
}
