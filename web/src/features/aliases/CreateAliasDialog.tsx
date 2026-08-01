import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import * as Dialog from "@radix-ui/react-dialog";
import { LoaderCircle, Plus, X } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";

import { api, getApiErrorMessage } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { CreatedAlias } from "../../api/schemas";
import { useNotifications } from "../../components/notificationContext";
import { createAliasSchema, type CreateAliasValues } from "./createAliasSchema";

const defaultValues: CreateAliasValues = { label: "" };

type CreateAliasDialogProps = {
  accountId: string;
  accountName: string;
  onCreated?: (alias: CreatedAlias) => void;
};

export function CreateAliasDialog({ accountId, accountName, onCreated }: CreateAliasDialogProps) {
  const [open, setOpen] = useState(false);
  const queryClient = useQueryClient();
  const { notify } = useNotifications();
  const {
    formState: { errors },
    handleSubmit,
    register,
    reset,
  } = useForm<CreateAliasValues>({
    defaultValues,
    resolver: zodResolver(createAliasSchema),
  });

  const createAlias = useMutation({
    mutationFn: (values: CreateAliasValues) =>
      api.createAlias({
        accountId,
        label: values.label || undefined,
      }),
    onSuccess: async (createdAlias) => {
      onCreated?.(createdAlias);
      reset(defaultValues);
      setOpen(false);
      notify({ message: createdAlias.email, title: "别名已创建", tone: "success" });
      await queryClient.invalidateQueries({ queryKey: queryKeys.aliases(accountId) });
    },
    retry: false,
  });

  function handleOpenChange(nextOpen: boolean) {
    if (createAlias.isPending) return;
    setOpen(nextOpen);
    if (!nextOpen) {
      reset(defaultValues);
      createAlias.reset();
    }
  }

  function submitAlias(values: CreateAliasValues) {
    if (createAlias.isPending) return;
    createAlias.reset();
    createAlias.mutate(values);
  }

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog.Trigger asChild>
        <button className="button button-primary" type="button">
          <Plus size={16} aria-hidden="true" />
          创建别名
        </button>
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog-content alias-dialog-content">
          <div className="dialog-heading-row">
            <div>
              <Dialog.Title className="dialog-title">创建别名</Dialog.Title>
              <Dialog.Description className="dialog-description">
                为“{accountName}”生成新的 Hide My Email 邮箱。
              </Dialog.Description>
            </div>
            <Dialog.Close asChild>
              <button
                className="icon-button"
                type="button"
                aria-label="关闭创建别名"
                title="关闭"
                disabled={createAlias.isPending}
              >
                <X size={17} aria-hidden="true" />
              </button>
            </Dialog.Close>
          </div>

          <form
            className="alias-form"
            noValidate
            onSubmit={(event) => void handleSubmit(submitAlias)(event)}
          >
            <div className="form-field">
              <label htmlFor="alias-label">标签（可选）</label>
              <input
                id="alias-label"
                autoComplete="off"
                aria-describedby={errors.label ? "alias-label-error" : undefined}
                aria-invalid={Boolean(errors.label)}
                placeholder="例如：新闻订阅"
                {...register("label")}
              />
              {errors.label ? (
                <span className="field-error" id="alias-label-error">
                  {errors.label.message}
                </span>
              ) : null}
            </div>

            {createAlias.isError ? (
              <div className="form-submit-error" role="alert">
                {getApiErrorMessage(createAlias.error)}
              </div>
            ) : null}

            <div className="dialog-actions">
              <Dialog.Close asChild>
                <button
                  className="button button-secondary"
                  type="button"
                  disabled={createAlias.isPending}
                >
                  取消
                </button>
              </Dialog.Close>
              <button
                className="button button-primary"
                type="submit"
                disabled={createAlias.isPending}
              >
                {createAlias.isPending ? (
                  <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
                ) : null}
                {createAlias.isPending ? "正在创建" : "创建别名"}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
