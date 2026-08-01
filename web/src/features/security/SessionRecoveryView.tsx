import { KeyRound, RefreshCw } from "lucide-react";
import type { ReactNode } from "react";
import { Link, useLocation } from "react-router-dom";

import { getApiErrorMessage, isSessionExpiredError } from "../../api/client";
import { ErrorState } from "../../components/ErrorState";
import { createSessionRecoveryLocationState } from "./sessionRecoveryState";

type SessionRecoveryLinkProps = {
  accountId: string;
  children?: ReactNode;
  className?: string;
};

export function SessionRecoveryLink({
  accountId,
  children = "更新 Cookie",
  className = "button button-primary session-recovery-button",
}: SessionRecoveryLinkProps) {
  const location = useLocation();
  const from = `${location.pathname}${location.search}`;

  return (
    <Link
      className={className}
      state={createSessionRecoveryLocationState(from)}
      to={`/accounts/${encodeURIComponent(accountId)}/security`}
    >
      <KeyRound size={14} aria-hidden="true" />
      {children}
    </Link>
  );
}

export function SessionExpiredState({
  accountId,
  error,
  title = "Cookie 会话已过期",
}: {
  accountId: string;
  error: unknown;
  title?: string;
}) {
  return (
    <ErrorState
      action={<SessionRecoveryLink accountId={accountId} />}
      description={getApiErrorMessage(error)}
      title={title}
    />
  );
}

export function AccountRequestErrorState({
  accountId,
  error,
  onRetry,
  title = "加载失败",
}: {
  accountId: string;
  error: unknown;
  onRetry?: () => void;
  title?: string;
}) {
  if (isSessionExpiredError(error)) {
    return <SessionExpiredState accountId={accountId} error={error} />;
  }

  return (
    <ErrorState
      action={
        onRetry ? (
          <button className="button button-secondary" type="button" onClick={onRetry}>
            <RefreshCw size={14} aria-hidden="true" />
            重新加载
          </button>
        ) : undefined
      }
      description={getApiErrorMessage(error)}
      title={title}
    />
  );
}
