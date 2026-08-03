import * as Dialog from "@radix-ui/react-dialog";
import { useQueryClient } from "@tanstack/react-query";
import { LoaderCircle, X } from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";

import { clearApiToken, setApiToken } from "../api/apiTokenSession";
import { createApiClient, getApiErrorMessage, isApiTokenError } from "../api/client";
import { ApiTokenContext } from "./apiTokenContext";

type ApiTokenProviderProps = {
  children: ReactNode;
};

export function ApiTokenProvider({ children }: ApiTokenProviderProps) {
  const queryClient = useQueryClient();
  const inputRef = useRef<HTMLInputElement>(null);
  const pendingTokenRef = useRef<string | undefined>(undefined);
  const [hasApiToken, setHasApiToken] = useState(false);
  const [isOpen, setIsOpen] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const clearCurrentApiToken = useCallback(() => {
    clearApiToken();
    pendingTokenRef.current = undefined;
    setHasApiToken(false);
    setIsOpen(false);
    setSubmitError(null);
    if (inputRef.current) {
      inputRef.current.value = "";
    }
  }, []);
  const openApiTokenDialog = useCallback(() => {
    setSubmitError(null);
    setIsOpen(true);
  }, [setIsOpen, setSubmitError]);

  useEffect(() => {
    const requireApiToken = () => {
      clearCurrentApiToken();
      setIsOpen(true);
    };
    const handleError = (error: unknown) => {
      if (isApiTokenError(error)) {
        requireApiToken();
      }
    };
    const unsubscribeQueries = queryClient.getQueryCache().subscribe((event) => {
      if (event.type === "updated") {
        handleError(event.query.state.error);
      }
    });
    const unsubscribeMutations = queryClient.getMutationCache().subscribe((event) => {
      if (event.type === "updated") {
        handleError(event.mutation.state.error);
      }
    });

    return () => {
      unsubscribeQueries();
      unsubscribeMutations();
    };
  }, [clearCurrentApiToken, queryClient, setIsOpen]);

  function handleOpenChange(nextOpen: boolean) {
    if (isSubmitting && !nextOpen) return;
    setIsOpen(nextOpen);
    if (!nextOpen) {
      pendingTokenRef.current = undefined;
      setSubmitError(null);
      if (inputRef.current) {
        inputRef.current.value = "";
      }
    }
  }

  async function submitApiToken(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (isSubmitting) return;

    const token = inputRef.current?.value.trim() ?? "";
    if (inputRef.current) {
      inputRef.current.value = "";
    }
    if (token === "") {
      setSubmitError("请输入 API 访问令牌。");
      inputRef.current?.focus();
      return;
    }

    pendingTokenRef.current = token;
    setSubmitError(null);
    setIsSubmitting(true);
    try {
      await createApiClient({ apiTokenProvider: () => pendingTokenRef.current }).getHealth();
      const verifiedToken = pendingTokenRef.current;
      if (!verifiedToken) {
        throw new Error("API 访问令牌验证未完成");
      }
      setApiToken(verifiedToken);
      pendingTokenRef.current = undefined;
      setHasApiToken(true);
      setIsOpen(false);
      void queryClient.resetQueries({ type: "active" });
    } catch (error) {
      pendingTokenRef.current = undefined;
      setSubmitError(
        isApiTokenError(error) ? "令牌无效或已过期，请检查后重试。" : getApiErrorMessage(error),
      );
      inputRef.current?.focus();
    } finally {
      setIsSubmitting(false);
    }
  }

  const contextValue = useMemo(
    () => ({ clearApiToken: clearCurrentApiToken, hasApiToken, openApiTokenDialog }),
    [clearCurrentApiToken, hasApiToken, openApiTokenDialog],
  );

  return (
    <ApiTokenContext.Provider value={contextValue}>
      {children}
      <Dialog.Root open={isOpen} onOpenChange={handleOpenChange}>
        <Dialog.Portal>
          <Dialog.Overlay className="dialog-overlay" />
          <Dialog.Content
            className="dialog-content api-token-dialog-content"
            onOpenAutoFocus={(event) => {
              event.preventDefault();
              inputRef.current?.focus();
            }}
          >
            <div className="dialog-heading-row">
              <div>
                <Dialog.Title className="dialog-title">API 访问令牌</Dialog.Title>
                <Dialog.Description className="dialog-description">
                  令牌仅保留在当前浏览器页面内，刷新或关闭页面后需要重新输入。
                </Dialog.Description>
              </div>
              <Dialog.Close asChild>
                <button
                  className="icon-button"
                  type="button"
                  aria-label="关闭 API 访问令牌"
                  title="关闭"
                  disabled={isSubmitting}
                >
                  <X size={17} aria-hidden="true" />
                </button>
              </Dialog.Close>
            </div>

            <form
              className="api-token-form"
              noValidate
              onSubmit={(event) => void submitApiToken(event)}
            >
              <div className="form-field">
                <label htmlFor="api-access-token">API 访问令牌</label>
                <input
                  id="api-access-token"
                  ref={inputRef}
                  autoCapitalize="none"
                  autoComplete="off"
                  autoCorrect="off"
                  aria-describedby={submitError ? "api-access-token-error" : undefined}
                  aria-invalid={Boolean(submitError)}
                  spellCheck={false}
                  type="password"
                />
                {submitError ? (
                  <span className="field-error" id="api-access-token-error" role="alert">
                    {submitError}
                  </span>
                ) : null}
              </div>

              <div className="dialog-actions">
                <Dialog.Close asChild>
                  <button className="button button-secondary" type="button" disabled={isSubmitting}>
                    取消
                  </button>
                </Dialog.Close>
                <button className="button button-primary" type="submit" disabled={isSubmitting}>
                  {isSubmitting ? (
                    <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
                  ) : null}
                  {isSubmitting ? "正在验证" : "验证并继续"}
                </button>
              </div>
            </form>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </ApiTokenContext.Provider>
  );
}
