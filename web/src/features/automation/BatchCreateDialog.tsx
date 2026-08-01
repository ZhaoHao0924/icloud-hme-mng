import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import * as Dialog from "@radix-ui/react-dialog";
import { LoaderCircle, Plus, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";

import { api, getApiErrorMessage } from "../../api/client";
import { queryKeys } from "../../api/queries";
import { useNotifications } from "../../components/notificationContext";
import { batchCreateFormSchema, type BatchCreateFormValues } from "./aliasAutomationSchema";

type BatchCreateDialogProps = {
  accountId: string;
  defaultLabelPrefix: string;
};

function defaultValues(labelPrefix: string): BatchCreateFormValues {
  return { count: 2, labelPrefix };
}

export function BatchCreateDialog({ accountId, defaultLabelPrefix }: BatchCreateDialogProps) {
  const [open, setOpen] = useState(false);
  const queryClient = useQueryClient();
  const { notify } = useNotifications();
  const {
    formState: { errors },
    handleSubmit,
    register,
    reset,
  } = useForm<BatchCreateFormValues>({
    defaultValues: defaultValues(defaultLabelPrefix),
    resolver: zodResolver(batchCreateFormSchema),
  });

  useEffect(() => {
    if (!open) reset(defaultValues(defaultLabelPrefix));
  }, [defaultLabelPrefix, open, reset]);

  const createBatch = useMutation({
    mutationFn: (values: BatchCreateFormValues) =>
      api.createAliasesBatch(accountId, {
        count: values.count,
        labelPrefix: values.labelPrefix || undefined,
      }),
    onSuccess: async (result) => {
      reset(defaultValues(defaultLabelPrefix));
      setOpen(false);
      notify({
        title: result.complete ? "批量创建已完成" : "批量创建部分完成",
        message: result.complete
          ? `已创建 ${result.created} 个别名`
          : `已创建 ${result.created} 个，失败 ${result.failed} 个`,
        tone: result.complete ? "success" : "warning",
      });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.aliases(accountId) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.accounts }),
      ]);
    },
    retry: false,
  });

  function handleOpenChange(nextOpen: boolean) {
    if (createBatch.isPending) return;
    setOpen(nextOpen);
    if (!nextOpen) {
      createBatch.reset();
      reset(defaultValues(defaultLabelPrefix));
    }
  }

  function submit(values: BatchCreateFormValues) {
    if (createBatch.isPending) return;
    createBatch.reset();
    createBatch.mutate(values);
  }

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog.Trigger asChild>
        <button className="button button-secondary" type="button">
          <Plus size={16} aria-hidden="true" />
          批量创建
        </button>
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog-content alias-dialog-content">
          <div className="dialog-heading-row">
            <div>
              <Dialog.Title className="dialog-title">批量创建别名</Dialog.Title>
              <Dialog.Description className="dialog-description">
                创建数量会按账户执行队列依次处理。
              </Dialog.Description>
            </div>
            <Dialog.Close asChild>
              <button
                className="icon-button"
                type="button"
                aria-label="关闭批量创建"
                title="关闭"
                disabled={createBatch.isPending}
              >
                <X size={17} aria-hidden="true" />
              </button>
            </Dialog.Close>
          </div>

          <form
            className="alias-form"
            noValidate
            onSubmit={(event) => void handleSubmit(submit)(event)}
          >
            <div className="form-field">
              <label htmlFor="batch-alias-count">创建数量</label>
              <input
                id="batch-alias-count"
                aria-describedby={errors.count ? "batch-alias-count-error" : undefined}
                aria-invalid={Boolean(errors.count)}
                disabled={createBatch.isPending}
                min={1}
                max={20}
                type="number"
                {...register("count", { valueAsNumber: true })}
              />
              {errors.count ? (
                <span className="field-error" id="batch-alias-count-error">
                  {errors.count.message}
                </span>
              ) : null}
            </div>

            <div className="form-field">
              <label htmlFor="batch-alias-prefix">标签前缀（可选）</label>
              <input
                id="batch-alias-prefix"
                autoComplete="off"
                aria-describedby={errors.labelPrefix ? "batch-alias-prefix-error" : undefined}
                aria-invalid={Boolean(errors.labelPrefix)}
                disabled={createBatch.isPending}
                maxLength={196}
                placeholder="例如：注册备用"
                {...register("labelPrefix")}
              />
              {errors.labelPrefix ? (
                <span className="field-error" id="batch-alias-prefix-error">
                  {errors.labelPrefix.message}
                </span>
              ) : null}
            </div>

            {createBatch.isError ? (
              <div className="form-submit-error" role="alert">
                {getApiErrorMessage(createBatch.error)}
              </div>
            ) : null}

            <div className="dialog-actions">
              <Dialog.Close asChild>
                <button
                  className="button button-secondary"
                  type="button"
                  disabled={createBatch.isPending}
                >
                  取消
                </button>
              </Dialog.Close>
              <button
                className="button button-primary"
                type="submit"
                disabled={createBatch.isPending}
              >
                {createBatch.isPending ? (
                  <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
                ) : null}
                {createBatch.isPending ? "正在创建" : "创建别名"}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
