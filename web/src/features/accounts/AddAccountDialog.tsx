import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import * as Dialog from "@radix-ui/react-dialog";
import { LoaderCircle, Plus, X } from "lucide-react";
import { useRef, useState } from "react";
import { useForm } from "react-hook-form";

import { api, getApiErrorMessage } from "../../api/client";
import { queryKeys } from "../../api/queries";
import { useNotifications } from "../../components/notificationContext";
import { addAccountSchema, type AddAccountValues } from "./addAccountSchema";

const defaultValues: AddAccountValues = {
  host: "icloud.com",
  icloudEmail: "",
  name: "",
  proxy: "",
};

type AddAccountDialogProps = {
  triggerLabel?: string;
};

export function AddAccountDialog({ triggerLabel = "添加账户" }: AddAccountDialogProps) {
  const [open, setOpen] = useState(false);
  const accountInFlight = useRef<AddAccountValues | null>(null);
  const queryClient = useQueryClient();
  const { notify } = useNotifications();
  const {
    formState: { errors },
    handleSubmit,
    register,
    reset,
  } = useForm<AddAccountValues>({
    defaultValues,
    resolver: zodResolver(addAccountSchema),
  });

  const createAccount = useMutation({
    mutationFn: async () => {
      const values = accountInFlight.current;
      if (!values) throw new Error("缺少账户提交数据");
      try {
        return await api.createAccount({
          host: values.host,
          icloudEmail: values.icloudEmail,
          name: values.name,
          proxy: values.proxy || undefined,
        });
      } finally {
        accountInFlight.current = null;
      }
    },
    onSuccess: async (account) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.accounts });
      reset(defaultValues);
      setOpen(false);
      notify({ title: `已添加账户“${account.name}”`, tone: "success" });
    },
  });

  function handleOpenChange(nextOpen: boolean) {
    if (createAccount.isPending) return;
    setOpen(nextOpen);
    if (!nextOpen) {
      accountInFlight.current = null;
      reset(defaultValues);
      createAccount.reset();
    }
  }

  function submitAccount(values: AddAccountValues) {
    if (createAccount.isPending) return;
    createAccount.reset();
    accountInFlight.current = values;
    createAccount.mutate();
  }

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog.Trigger asChild>
        <button className="button button-primary" type="button">
          <Plus size={16} aria-hidden="true" />
          {triggerLabel}
        </button>
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog-content account-dialog-content">
          <div className="dialog-heading-row">
            <div>
              <Dialog.Title className="dialog-title">添加账户</Dialog.Title>
              <Dialog.Description className="dialog-description">
                创建后可在凭据页完成 Cookie、App 密码或 Apple 登录。
              </Dialog.Description>
            </div>
            <Dialog.Close asChild>
              <button
                className="icon-button"
                type="button"
                aria-label="关闭添加账户"
                title="关闭"
                disabled={createAccount.isPending}
              >
                <X size={17} aria-hidden="true" />
              </button>
            </Dialog.Close>
          </div>

          <form
            className="account-form"
            noValidate
            onSubmit={(event) => void handleSubmit(submitAccount)(event)}
          >
            <div className="form-field">
              <label htmlFor="account-name">账户名称</label>
              <input
                id="account-name"
                autoComplete="off"
                aria-describedby={errors.name ? "account-name-error" : undefined}
                aria-invalid={Boolean(errors.name)}
                placeholder="例如：主账号"
                {...register("name")}
              />
              {errors.name ? (
                <span className="field-error" id="account-name-error">
                  {errors.name.message}
                </span>
              ) : null}
            </div>

            <div className="form-field">
              <label htmlFor="account-email">iCloud 邮箱</label>
              <input
                id="account-email"
                autoCapitalize="none"
                autoComplete="email"
                aria-describedby={errors.icloudEmail ? "account-email-error" : undefined}
                aria-invalid={Boolean(errors.icloudEmail)}
                inputMode="email"
                placeholder="name@icloud.com"
                {...register("icloudEmail")}
              />
              {errors.icloudEmail ? (
                <span className="field-error" id="account-email-error">
                  {errors.icloudEmail.message}
                </span>
              ) : null}
            </div>

            <div className="form-field">
              <label htmlFor="account-host">区域</label>
              <select id="account-host" {...register("host")}>
                <option value="icloud.com">全球（icloud.com）</option>
                <option value="icloud.com.cn">中国大陆（icloud.com.cn）</option>
              </select>
            </div>

            <div className="form-field">
              <label htmlFor="account-proxy">代理（可选）</label>
              <input
                id="account-proxy"
                autoCapitalize="none"
                autoComplete="off"
                aria-describedby={errors.proxy ? "account-proxy-error" : undefined}
                aria-invalid={Boolean(errors.proxy)}
                placeholder="http://host:port"
                {...register("proxy")}
              />
              {errors.proxy ? (
                <span className="field-error" id="account-proxy-error">
                  {errors.proxy.message}
                </span>
              ) : null}
            </div>

            {createAccount.isError ? (
              <div className="form-submit-error" role="alert">
                {getApiErrorMessage(createAccount.error)}
              </div>
            ) : null}

            <div className="dialog-actions">
              <Dialog.Close asChild>
                <button
                  className="button button-secondary"
                  type="button"
                  disabled={createAccount.isPending}
                >
                  取消
                </button>
              </Dialog.Close>
              <button
                className="button button-primary"
                type="submit"
                disabled={createAccount.isPending}
              >
                {createAccount.isPending ? (
                  <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
                ) : null}
                {createAccount.isPending ? "正在添加" : "添加账户"}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
