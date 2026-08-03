import { LoaderCircle, ShieldCheck } from "lucide-react";
import { useRef, useState, type FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router-dom";

import { api, createApiClient, getApiErrorMessage } from "../api/client";
import { LoadingState } from "../components/LoadingState";
import { usePlatformAuth } from "./platformAuthContext";

function returnPath(value: unknown) {
  return typeof value === "string" && value.startsWith("/") && !value.startsWith("//")
    ? value
    : "/accounts";
}

export function PlatformLoginPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const { error: sessionError, isLoading, setAuthenticated, status } = usePlatformAuth();
  const usernameRef = useRef<HTMLInputElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);
  const confirmationRef = useRef<HTMLInputElement>(null);
  const apiTokenRef = useRef<HTMLInputElement>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const isSetup = status?.configured === false;
  const destination = returnPath((location.state as { from?: unknown } | null)?.from);

  if (isLoading) {
    return (
      <main className="platform-auth-page">
        <LoadingState label="正在检查登录状态" />
      </main>
    );
  }

  if (sessionError) {
    return (
      <main className="platform-auth-page">
        <section
          className="platform-auth-panel platform-auth-unavailable"
          aria-labelledby="platform-login-error-title"
        >
          <h1 id="platform-login-error-title">无法连接登录服务</h1>
          <p>{getApiErrorMessage(sessionError)}</p>
        </section>
      </main>
    );
  }

  if (status?.authenticated) {
    return <Navigate replace to={destination} />;
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (isSubmitting) return;

    const username = usernameRef.current?.value.trim() ?? "";
    const password = passwordRef.current?.value ?? "";
    const confirmation = confirmationRef.current?.value ?? "";
    const apiToken = apiTokenRef.current?.value.trim() ?? "";
    if (passwordRef.current) passwordRef.current.value = "";
    if (confirmationRef.current) confirmationRef.current.value = "";
    if (apiTokenRef.current) apiTokenRef.current.value = "";

    if (isSetup && password !== confirmation) {
      setFormError("两次输入的密码不一致。");
      confirmationRef.current?.focus();
      return;
    }

    setFormError(null);
    setIsSubmitting(true);
    try {
      const client = apiToken === "" ? api : createApiClient({ apiTokenProvider: () => apiToken });
      const nextStatus = isSetup
        ? await client.setupPlatformAuth({ password, username })
        : await api.loginPlatform({ password, username });
      setAuthenticated(nextStatus);
      navigate(destination, { replace: true });
    } catch (requestError) {
      setFormError(getApiErrorMessage(requestError));
      passwordRef.current?.focus();
    } finally {
      setIsSubmitting(false);
    }
  }

  const title = isSetup ? "创建管理员账户" : "登录平台";
  const submitLabel = isSetup ? "创建并进入平台" : "登录";

  return (
    <main className="platform-auth-page">
      <section className="platform-auth-panel" aria-labelledby="platform-login-title">
        <div className="platform-auth-brand">
          <span className="platform-auth-mark" aria-hidden="true">
            <ShieldCheck size={24} strokeWidth={2} />
          </span>
          <span>iCloud HME</span>
        </div>
        <div className="platform-auth-heading">
          <h1 id="platform-login-title">{title}</h1>
        </div>
        <form className="platform-auth-form" noValidate onSubmit={(event) => void submit(event)}>
          <div className="form-field">
            <label htmlFor="platform-username">管理员账号</label>
            <input
              id="platform-username"
              ref={usernameRef}
              autoCapitalize="none"
              autoComplete="username"
              autoCorrect="off"
              defaultValue="admin"
              disabled={isSubmitting}
              maxLength={32}
              required
              spellCheck={false}
            />
          </div>
          <div className="form-field">
            <label htmlFor="platform-password">管理员密码</label>
            <input
              id="platform-password"
              ref={passwordRef}
              autoComplete={isSetup ? "new-password" : "current-password"}
              disabled={isSubmitting}
              maxLength={72}
              minLength={12}
              required
              type="password"
            />
          </div>
          {isSetup ? (
            <>
              <div className="form-field">
                <label htmlFor="platform-password-confirmation">确认管理员密码</label>
                <input
                  id="platform-password-confirmation"
                  ref={confirmationRef}
                  autoComplete="new-password"
                  disabled={isSubmitting}
                  maxLength={72}
                  minLength={12}
                  required
                  type="password"
                />
              </div>
              <div className="form-field">
                <label htmlFor="platform-api-token">API 访问令牌（远程部署时需要）</label>
                <input
                  id="platform-api-token"
                  ref={apiTokenRef}
                  autoCapitalize="none"
                  autoComplete="off"
                  autoCorrect="off"
                  disabled={isSubmitting}
                  spellCheck={false}
                  type="password"
                />
              </div>
            </>
          ) : null}
          {formError ? (
            <p className="platform-auth-error" role="alert">
              {formError}
            </p>
          ) : null}
          <button
            className="button button-primary platform-auth-submit"
            disabled={isSubmitting}
            type="submit"
          >
            {isSubmitting ? (
              <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
            ) : null}
            {isSubmitting ? "正在验证" : submitLabel}
          </button>
        </form>
      </section>
    </main>
  );
}
