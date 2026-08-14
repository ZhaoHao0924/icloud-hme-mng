import { zodResolver } from "@hookform/resolvers/zod";
import * as Dialog from "@radix-ui/react-dialog";
import { type QueryClient, useMutation, useQueryClient } from "@tanstack/react-query";
import { Eye, EyeOff, LoaderCircle, LogIn, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";

import { api, getApiErrorMessage } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { Account } from "../../api/schemas";
import { useNotifications } from "../../components/notificationContext";
import {
  appleLoginSchema,
  otpCodeSchema,
  type AppleLoginFormValues,
  type OtpCodeFormValues,
} from "./appleLoginSchema";
import { CredentialCapabilityStatus } from "./CredentialCapabilityStatus";

type PendingOtpChallenge = {
  challengeId: string;
  expiresAt: number;
};

type StartLoginMutationResult =
  { account: Account; status: "authenticated" } | { expiresIn: number; status: "otp_required" };

type OtpSubmission = {
  challengeId: string;
  otpCode: string;
};

const otpDefaultValues: OtpCodeFormValues = { otpCode: "" };

async function invalidateSessionCredentialQueries(queryClient: QueryClient, accountId: string) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: queryKeys.accounts }),
    queryClient.invalidateQueries({ queryKey: queryKeys.aliasAutomation(accountId) }),
    queryClient.invalidateQueries({ queryKey: queryKeys.aliases(accountId) }),
  ]);
}

function getAppleLoginErrorMessage(error: unknown) {
  return getApiErrorMessage(error);
}

function formatCountdown(totalSeconds: number) {
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = String(totalSeconds % 60).padStart(2, "0");
  return `${minutes}:${seconds}`;
}

export function AppleLoginSection({
  account,
  onAuthenticated,
}: {
  account: Account;
  onAuthenticated?: () => void;
}) {
  const queryClient = useQueryClient();
  const { notify } = useNotifications();
  const loginButtonRef = useRef<HTMLButtonElement>(null);
  const passwordInFlight = useRef<string | null>(null);
  const issuedChallengeId = useRef<string | null>(null);
  const otpInFlight = useRef<OtpSubmission | null>(null);
  const [passwordVisible, setPasswordVisible] = useState(false);
  const [pendingChallenge, setPendingChallenge] = useState<PendingOtpChallenge | null>(null);
  const [remainingSeconds, setRemainingSeconds] = useState(0);
  const [challengeNotice, setChallengeNotice] = useState<string | null>(null);
  const {
    formState: { errors: passwordErrors },
    handleSubmit: handlePasswordSubmit,
    register: registerPassword,
    reset: resetPassword,
  } = useForm<AppleLoginFormValues>({
    defaultValues: { password: "" },
    resolver: zodResolver(appleLoginSchema),
  });
  const {
    formState: { errors: otpErrors },
    handleSubmit: handleOtpSubmit,
    register: registerOtp,
    reset: resetOtp,
  } = useForm<OtpCodeFormValues>({
    defaultValues: otpDefaultValues,
    resolver: zodResolver(otpCodeSchema),
  });

  const verifyLogin = useMutation({
    mutationFn: async () => {
      const submission = otpInFlight.current;
      if (!submission) throw new Error("缺少验证码提交数据");
      try {
        return await api.verifyLogin(account.id, submission.challengeId, submission.otpCode);
      } finally {
        otpInFlight.current = null;
      }
    },
    onError: handleVerifyError,
    onSuccess: handleVerifySuccess,
  });

  const loginWithPassword = useMutation({
    mutationFn: async (): Promise<StartLoginMutationResult> => {
      const password = passwordInFlight.current;
      if (!password) throw new Error("缺少 Apple ID 密码提交数据");
      try {
        const result = await api.startLogin(account.id, password);
        if (result.status === "otp_required") {
          issuedChallengeId.current = result.challenge_id;
          return { expiresIn: result.expires_in, status: "otp_required" };
        }
        return { account: result, status: "authenticated" };
      } finally {
        passwordInFlight.current = null;
      }
    },
    onError: () => {
      issuedChallengeId.current = null;
    },
    onSuccess: handleLoginSuccess,
  });

  async function handleLoginSuccess(result: StartLoginMutationResult) {
    if (result.status === "otp_required") {
      const challengeId = issuedChallengeId.current;
      issuedChallengeId.current = null;
      if (!challengeId) {
        setChallengeNotice("登录验证状态无效，请重新提交 Apple ID 密码。");
        loginWithPassword.reset();
        return;
      }
      resetOtp(otpDefaultValues);
      verifyLogin.reset();
      setChallengeNotice(null);
      setRemainingSeconds(result.expiresIn);
      setPendingChallenge({
        challengeId,
        expiresAt: Date.now() + result.expiresIn * 1000,
      });
      loginWithPassword.reset();
      return;
    }

    await invalidateSessionCredentialQueries(queryClient, account.id);
    notify({ title: "Apple 登录成功", message: result.account.name, tone: "success" });
    onAuthenticated?.();
    loginWithPassword.reset();
  }

  async function handleVerifySuccess(updatedAccount: Account) {
    setPendingChallenge(null);
    setChallengeNotice(null);
    resetOtp(otpDefaultValues);
    await invalidateSessionCredentialQueries(queryClient, account.id);
    notify({ title: "Apple 登录成功", message: updatedAccount.name, tone: "success" });
    onAuthenticated?.();
    verifyLogin.reset();
  }

  function handleVerifyError() {
    setPendingChallenge(null);
    resetOtp(otpDefaultValues);
  }

  const expireChallenge = useCallback(() => {
    setPendingChallenge(null);
    setRemainingSeconds(0);
    resetOtp(otpDefaultValues);
    setChallengeNotice("验证码已过期，请重新提交 Apple ID 密码。");
  }, [resetOtp]);

  useEffect(() => {
    if (!pendingChallenge || verifyLogin.isPending) return;
    const expiresAt = pendingChallenge.expiresAt;

    function updateRemaining() {
      setRemainingSeconds(Math.max(0, Math.ceil((expiresAt - Date.now()) / 1000)));
    }

    updateRemaining();
    const interval = window.setInterval(updateRemaining, 1000);
    const timeout = window.setTimeout(expireChallenge, Math.max(0, expiresAt - Date.now()));
    return () => {
      window.clearInterval(interval);
      window.clearTimeout(timeout);
    };
  }, [expireChallenge, pendingChallenge, verifyLogin.isPending]);

  function submitAppleLogin(values: AppleLoginFormValues) {
    if (loginWithPassword.isPending) return;
    setPendingChallenge(null);
    setChallengeNotice(null);
    resetOtp(otpDefaultValues);
    verifyLogin.reset();
    loginWithPassword.reset();
    issuedChallengeId.current = null;
    passwordInFlight.current = values.password;
    resetPassword({ password: "" });
    setPasswordVisible(false);
    loginWithPassword.mutate();
  }

  function submitOtp(values: OtpCodeFormValues) {
    if (verifyLogin.isPending) return;
    if (!pendingChallenge || Date.now() >= pendingChallenge.expiresAt) {
      expireChallenge();
      return;
    }

    verifyLogin.reset();
    otpInFlight.current = {
      challengeId: pendingChallenge.challengeId,
      otpCode: values.otpCode,
    };
    resetOtp(otpDefaultValues);
    verifyLogin.mutate();
  }

  function handleOtpOpenChange(nextOpen: boolean) {
    if (nextOpen || verifyLogin.isPending) return;
    setPendingChallenge(null);
    setRemainingSeconds(0);
    otpInFlight.current = null;
    resetOtp(otpDefaultValues);
    verifyLogin.reset();
    setChallengeNotice("验证码验证已取消。");
  }

  const loginError = verifyLogin.isError
    ? verifyLogin.error
    : loginWithPassword.isError
      ? loginWithPassword.error
      : null;

  return (
    <>
      <section className="credential-section" aria-labelledby="apple-login-section-title">
        <div className="credential-section-heading">
          <div>
            <span className="credential-section-icon" aria-hidden="true">
              <LogIn size={18} />
            </span>
            <h3 id="apple-login-section-title">Apple 登录</h3>
          </div>
          <CredentialCapabilityStatus
            configured={account.has_cookies}
            configuredLabel="Cookie 已配置"
            missingLabel="Cookie 未配置"
          />
        </div>

        <form
          className="credential-form"
          noValidate
          onSubmit={(event) => void handlePasswordSubmit(submitAppleLogin)(event)}
        >
          <div className="form-field">
            <div className="form-label-row">
              <label htmlFor="apple-login-password">Apple ID 密码</label>
              <button
                className="icon-button secret-visibility-button"
                type="button"
                aria-label={passwordVisible ? "隐藏 Apple ID 密码" : "显示 Apple ID 密码"}
                title={passwordVisible ? "隐藏 Apple ID 密码" : "显示 Apple ID 密码"}
                disabled={loginWithPassword.isPending}
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
              id="apple-login-password"
              type={passwordVisible ? "text" : "password"}
              autoCapitalize="none"
              autoComplete="current-password"
              aria-describedby={passwordErrors.password ? "apple-login-password-error" : undefined}
              aria-invalid={Boolean(passwordErrors.password)}
              disabled={loginWithPassword.isPending}
              spellCheck={false}
              {...registerPassword("password")}
            />
            {passwordErrors.password ? (
              <span className="field-error" id="apple-login-password-error">
                {passwordErrors.password.message}
              </span>
            ) : null}
          </div>

          {challengeNotice ? (
            <div className="form-submit-notice" role="status">
              {challengeNotice}
            </div>
          ) : null}

          {loginError ? (
            <div className="form-submit-error" role="alert">
              {getAppleLoginErrorMessage(loginError)}
            </div>
          ) : null}

          <div className="credential-form-actions">
            <button
              className="button button-primary"
              ref={loginButtonRef}
              type="submit"
              disabled={loginWithPassword.isPending}
            >
              {loginWithPassword.isPending ? (
                <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
              ) : null}
              {loginWithPassword.isPending ? "正在登录" : "登录"}
            </button>
          </div>
        </form>
      </section>

      <Dialog.Root open={pendingChallenge !== null} onOpenChange={handleOtpOpenChange}>
        <Dialog.Portal>
          <Dialog.Overlay className="dialog-overlay" />
          <Dialog.Content
            className="dialog-content otp-dialog-content"
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              loginButtonRef.current?.focus();
            }}
          >
            <div className="dialog-heading-row">
              <div>
                <Dialog.Title className="dialog-title">验证 Apple 登录</Dialog.Title>
                <Dialog.Description className="dialog-description">
                  双重认证 · {formatCountdown(remainingSeconds)} 后过期
                </Dialog.Description>
              </div>
              <Dialog.Close asChild>
                <button
                  className="icon-button"
                  type="button"
                  aria-label="关闭验证码验证"
                  title="关闭"
                  disabled={verifyLogin.isPending}
                >
                  <X size={17} aria-hidden="true" />
                </button>
              </Dialog.Close>
            </div>

            <form
              className="otp-form"
              noValidate
              onSubmit={(event) => void handleOtpSubmit(submitOtp)(event)}
            >
              <div className="form-field">
                <label htmlFor="apple-login-otp">6 位验证码</label>
                <input
                  id="apple-login-otp"
                  type="text"
                  autoComplete="one-time-code"
                  autoFocus
                  enterKeyHint="done"
                  inputMode="numeric"
                  maxLength={6}
                  aria-describedby={otpErrors.otpCode ? "apple-login-otp-error" : undefined}
                  aria-invalid={Boolean(otpErrors.otpCode)}
                  disabled={verifyLogin.isPending}
                  {...registerOtp("otpCode")}
                />
                {otpErrors.otpCode ? (
                  <span className="field-error" id="apple-login-otp-error">
                    {otpErrors.otpCode.message}
                  </span>
                ) : null}
              </div>

              <div className="dialog-actions">
                <Dialog.Close asChild>
                  <button
                    className="button button-secondary"
                    type="button"
                    disabled={verifyLogin.isPending}
                  >
                    取消
                  </button>
                </Dialog.Close>
                <button
                  className="button button-primary"
                  type="submit"
                  disabled={verifyLogin.isPending}
                >
                  {verifyLogin.isPending ? (
                    <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
                  ) : null}
                  {verifyLogin.isPending ? "正在验证" : "验证"}
                </button>
              </div>
            </form>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </>
  );
}
