import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Cookie, Eye, EyeOff, KeyRound, LoaderCircle } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { useLocation, useNavigate } from "react-router-dom";

import { api, getApiErrorMessage } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { Account } from "../../api/schemas";
import { ErrorState } from "../../components/ErrorState";
import { useNotifications } from "../../components/notificationContext";
import { useAccountDetailContext } from "../accounts/accountDetailContext";
import { AppleLoginSection } from "./AppleLoginSection";
import { appPasswordSchema, type AppPasswordFormValues } from "./appPasswordSchema";
import { CookieInputError, parseCookieInput } from "./cookieInput";
import { CredentialCapabilityStatus } from "./CredentialCapabilityStatus";
import { readSessionRecoveryLocationState } from "./sessionRecoveryState";

type CookieFormValues = {
  cookieInput: string;
};

const cookieDefaultValues: CookieFormValues = { cookieInput: "" };

function CookieSection({
  account,
  focusOnMount = false,
  onSessionRestored,
}: {
  account: Account;
  focusOnMount?: boolean;
  onSessionRestored?: () => void;
}) {
  const queryClient = useQueryClient();
  const { notify } = useNotifications();
  const cookiesInFlight = useRef<Record<string, string> | null>(null);
  const [cookieVisible, setCookieVisible] = useState(false);
  const {
    clearErrors,
    formState: { errors },
    handleSubmit,
    register,
    reset,
    setError,
    setFocus,
  } = useForm<CookieFormValues>({ defaultValues: cookieDefaultValues });

  useEffect(() => {
    if (focusOnMount) setFocus("cookieInput");
  }, [focusOnMount, setFocus]);

  const updateCookies = useMutation({
    mutationFn: async () => {
      const cookies = cookiesInFlight.current;
      if (!cookies) throw new CookieInputError("请输入 Cookie");
      try {
        return await api.updateCookies(account.id, cookies);
      } finally {
        cookiesInFlight.current = null;
      }
    },
    onSuccess: async (updatedAccount) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.accounts });
      notify({ title: "Cookie 已更新", message: updatedAccount.name, tone: "success" });
      onSessionRestored?.();
    },
  });

  function submitCookies(values: CookieFormValues) {
    if (updateCookies.isPending) return;
    let cookies: Record<string, string>;
    try {
      cookies = parseCookieInput(values.cookieInput);
    } catch (error) {
      setError("cookieInput", {
        message: error instanceof CookieInputError ? error.message : "Cookie 格式无效",
        type: "validate",
      });
      return;
    }

    clearErrors("cookieInput");
    updateCookies.reset();
    cookiesInFlight.current = cookies;
    reset(cookieDefaultValues);
    setCookieVisible(false);
    updateCookies.mutate();
  }

  return (
    <section className="credential-section" aria-labelledby="cookie-section-title">
      <div className="credential-section-heading">
        <div>
          <span className="credential-section-icon" aria-hidden="true">
            <Cookie size={18} />
          </span>
          <h3 id="cookie-section-title">Cookie</h3>
        </div>
        <CredentialCapabilityStatus configured={account.has_cookies} />
      </div>

      <form
        className="credential-form"
        noValidate
        onSubmit={(event) => void handleSubmit(submitCookies)(event)}
      >
        <div className="form-field">
          <div className="form-label-row">
            <label htmlFor="cookie-input">Cookie 数据</label>
            <button
              className="icon-button secret-visibility-button"
              type="button"
              aria-label={cookieVisible ? "隐藏 Cookie" : "显示 Cookie"}
              title={cookieVisible ? "隐藏 Cookie" : "显示 Cookie"}
              disabled={updateCookies.isPending}
              onClick={() => setCookieVisible((visible) => !visible)}
            >
              {cookieVisible ? (
                <EyeOff size={16} aria-hidden="true" />
              ) : (
                <Eye size={16} aria-hidden="true" />
              )}
            </button>
          </div>
          <textarea
            id="cookie-input"
            autoCapitalize="none"
            autoComplete="off"
            aria-describedby={errors.cookieInput ? "cookie-input-error" : undefined}
            aria-invalid={Boolean(errors.cookieInput)}
            className={cookieVisible ? undefined : "secret-input-concealed"}
            disabled={updateCookies.isPending}
            rows={8}
            spellCheck={false}
            {...register("cookieInput")}
          />
          {errors.cookieInput ? (
            <span className="field-error" id="cookie-input-error">
              {errors.cookieInput.message}
            </span>
          ) : null}
        </div>

        {updateCookies.isError ? (
          <div className="form-submit-error" role="alert">
            {getApiErrorMessage(updateCookies.error)}
          </div>
        ) : null}

        <div className="credential-form-actions">
          <button
            className="button button-primary"
            type="submit"
            disabled={updateCookies.isPending}
          >
            {updateCookies.isPending ? (
              <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
            ) : null}
            {updateCookies.isPending ? "正在校验" : "更新 Cookie"}
          </button>
        </div>
      </form>
    </section>
  );
}

function AppPasswordSection({ account }: { account: Account }) {
  const queryClient = useQueryClient();
  const { notify } = useNotifications();
  const credentialsInFlight = useRef<AppPasswordFormValues | null>(null);
  const [passwordVisible, setPasswordVisible] = useState(false);
  const {
    formState: { errors },
    handleSubmit,
    register,
    reset,
  } = useForm<AppPasswordFormValues>({
    defaultValues: {
      appPassword: "",
      icloudEmail: account.icloud_email,
    },
    resolver: zodResolver(appPasswordSchema),
  });

  const updateAppPassword = useMutation({
    mutationFn: async () => {
      const credentials = credentialsInFlight.current;
      if (!credentials) throw new Error("缺少 App 专用密码提交数据");
      try {
        return await api.setAppPassword(account.id, credentials);
      } finally {
        credentialsInFlight.current = null;
      }
    },
    onSuccess: async (updatedAccount) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.accounts });
      notify({
        title: "App 密码已验证",
        message: updatedAccount.name,
        tone: "success",
      });
    },
  });

  function submitAppPassword(values: AppPasswordFormValues) {
    if (updateAppPassword.isPending) return;
    updateAppPassword.reset();
    credentialsInFlight.current = values;
    reset({ appPassword: "", icloudEmail: values.icloudEmail });
    setPasswordVisible(false);
    updateAppPassword.mutate();
  }

  return (
    <section className="credential-section" aria-labelledby="app-password-section-title">
      <div className="credential-section-heading">
        <div>
          <span className="credential-section-icon" aria-hidden="true">
            <KeyRound size={18} />
          </span>
          <h3 id="app-password-section-title">App 专用密码</h3>
        </div>
        <CredentialCapabilityStatus configured={account.has_app_password} />
      </div>

      <form
        className="credential-form"
        noValidate
        onSubmit={(event) => void handleSubmit(submitAppPassword)(event)}
      >
        <div className="form-field">
          <label htmlFor="app-password-email">iCloud 邮箱</label>
          <input
            id="app-password-email"
            type="email"
            autoCapitalize="none"
            autoComplete="email"
            aria-describedby={errors.icloudEmail ? "app-password-email-error" : undefined}
            aria-invalid={Boolean(errors.icloudEmail)}
            disabled={updateAppPassword.isPending}
            inputMode="email"
            spellCheck={false}
            {...register("icloudEmail")}
          />
          {errors.icloudEmail ? (
            <span className="field-error" id="app-password-email-error">
              {errors.icloudEmail.message}
            </span>
          ) : null}
        </div>

        <div className="form-field">
          <div className="form-label-row">
            <label htmlFor="app-password-input">App 专用密码</label>
            <button
              className="icon-button secret-visibility-button"
              type="button"
              aria-label={passwordVisible ? "隐藏 App 专用密码" : "显示 App 专用密码"}
              title={passwordVisible ? "隐藏 App 专用密码" : "显示 App 专用密码"}
              disabled={updateAppPassword.isPending}
              onClick={() => setPasswordVisible((visible) => !visible)}
            >
              {passwordVisible ? (
                <EyeOff size={16} aria-hidden="true" />
              ) : (
                <Eye size={16} aria-hidden="true" />
              )}
            </button>
          </div>
          <input
            id="app-password-input"
            type={passwordVisible ? "text" : "password"}
            autoCapitalize="none"
            autoComplete="new-password"
            aria-describedby={errors.appPassword ? "app-password-input-error" : undefined}
            aria-invalid={Boolean(errors.appPassword)}
            disabled={updateAppPassword.isPending}
            spellCheck={false}
            {...register("appPassword")}
          />
          {errors.appPassword ? (
            <span className="field-error" id="app-password-input-error">
              {errors.appPassword.message}
            </span>
          ) : null}
        </div>

        {updateAppPassword.isError ? (
          <div className="form-submit-error" role="alert">
            {getApiErrorMessage(updateAppPassword.error)}
          </div>
        ) : null}

        <div className="credential-form-actions">
          <button
            className="button button-primary"
            type="submit"
            disabled={updateAppPassword.isPending}
          >
            {updateAppPassword.isPending ? (
              <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
            ) : null}
            {updateAppPassword.isPending ? "正在验证" : "验证并保存"}
          </button>
        </div>
      </form>
    </section>
  );
}

export function SecurityPage() {
  const { account } = useAccountDetailContext();
  const location = useLocation();
  const navigate = useNavigate();
  const sessionRecovery = readSessionRecoveryLocationState(location.state);

  function completeSessionRecovery() {
    if (sessionRecovery) navigate(sessionRecovery.from, { replace: true });
  }

  return (
    <div className="credential-page">
      {sessionRecovery ? (
        <ErrorState
          description="更新 Cookie 或重新登录后将返回原页面。"
          title="Cookie 会话已过期"
        />
      ) : null}
      <CookieSection
        account={account}
        focusOnMount={sessionRecovery !== null}
        key={`cookie-${account.id}`}
        onSessionRestored={completeSessionRecovery}
      />
      <AppPasswordSection account={account} key={`app-password-${account.id}`} />
      <AppleLoginSection
        account={account}
        key={`apple-login-${account.id}`}
        onAuthenticated={completeSessionRecovery}
      />
    </div>
  );
}
