import { ArrowLeft, AtSign, Bot, Inbox, KeyRound } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { Link, NavLink, Outlet, useNavigate, useParams } from "react-router-dom";

import { getApiErrorMessage } from "../../api/client";
import { accountsQueryOptions } from "../../api/queries";
import { ErrorState } from "../../components/ErrorState";
import { LoadingState } from "../../components/LoadingState";
import { useNotifications } from "../../components/notificationContext";
import { accountStatusMeta } from "./accountPresentation";

const detailTabs = [
  { icon: AtSign, label: "别名", segment: "aliases" },
  { icon: Bot, label: "自动化", segment: "automation" },
  { icon: Inbox, label: "收件箱", segment: "inbox" },
  { icon: KeyRound, label: "凭据", segment: "security" },
] as const;

export function AccountDetailLayout() {
  const { accountId = "" } = useParams<{ accountId: string }>();
  const navigate = useNavigate();
  const { notify } = useNotifications();
  const accountsQuery = useQuery(accountsQueryOptions());
  const account = accountsQuery.data?.find((candidate) => candidate.id === accountId);
  const lastNotifiedAccountId = useRef<string | null>(null);

  useEffect(() => {
    if (!accountsQuery.isSuccess || account || lastNotifiedAccountId.current === accountId) {
      return;
    }

    lastNotifiedAccountId.current = accountId;
    notify({
      title: "账户不存在",
      message: "请从账户列表重新选择一个账户。",
      tone: "error",
    });
    navigate("/accounts", { replace: true });
  }, [account, accountId, accountsQuery.isSuccess, navigate, notify]);

  if (accountsQuery.isPending) {
    return <LoadingState label="正在读取账户上下文" />;
  }

  if (accountsQuery.isError) {
    return (
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
    );
  }

  if (!account) {
    return <LoadingState label="正在返回账户列表" />;
  }

  const status = accountStatusMeta(account.status);

  return (
    <section className="account-detail" aria-labelledby="account-detail-title">
      <div className="account-detail-header">
        <Link className="account-back-link" to="/accounts">
          <ArrowLeft size={16} aria-hidden="true" />
          返回账户
        </Link>
        <div className="account-context">
          <div className="account-context-copy">
            <span className="account-context-eyebrow">账户上下文</span>
            <h2 id="account-detail-title">{account.name}</h2>
            <span className="account-context-meta">
              {account.icloud_email || "未设置 iCloud 邮箱"}
              <span aria-hidden="true"> · </span>
              {account.id}
            </span>
          </div>
          <div className="account-context-status">
            <span className={`status-chip status-chip-${status.tone}`}>
              <span className="status-chip-dot" aria-hidden="true" />
              {status.label}
            </span>
            {account.last_error ? (
              <span className="account-context-error" title={account.last_error}>
                {account.last_error}
              </span>
            ) : null}
          </div>
        </div>
      </div>

      <nav className="account-detail-tabs" aria-label={`${account.name}详情导航`}>
        {detailTabs.map(({ icon: Icon, label, segment }) => (
          <NavLink
            className={({ isActive }) =>
              `account-detail-tab${isActive ? " account-detail-tab-active" : ""}`
            }
            end
            key={segment}
            to={`/accounts/${encodeURIComponent(account.id)}/${segment}`}
          >
            <Icon size={16} aria-hidden="true" />
            {label}
          </NavLink>
        ))}
      </nav>

      <div className="account-detail-content">
        <Outlet context={{ account }} />
      </div>
    </section>
  );
}
