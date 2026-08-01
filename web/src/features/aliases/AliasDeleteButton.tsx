import { RefreshCw, Trash2 } from "lucide-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";

import { api, getApiErrorMessage } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { Alias } from "../../api/schemas";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { useNotifications } from "../../components/notificationContext";

type AliasDeleteButtonProps = {
  accountId: string;
  alias: Alias;
};

export function AliasDeleteButton({ accountId, alias }: AliasDeleteButtonProps) {
  const [open, setOpen] = useState(false);
  const deleteButtonRef = useRef<HTMLButtonElement>(null);
  const queryClient = useQueryClient();
  const { notify } = useNotifications();
  const deleteAlias = useMutation({
    mutationFn: () => api.deleteAlias(accountId, alias.anonymousId),
    onSuccess: () => {
      setOpen(false);
      notify({ message: alias.email, title: "别名已删除", tone: "success" });
      void queryClient.invalidateQueries({ queryKey: queryKeys.aliases(accountId) });
    },
    retry: false,
  });

  function handleOpenChange(nextOpen: boolean) {
    if (deleteAlias.isPending) return;
    setOpen(nextOpen);
    if (!nextOpen) {
      deleteAlias.reset();
    }
  }

  return (
    <>
      <button
        className="icon-button alias-delete-button"
        ref={deleteButtonRef}
        type="button"
        aria-label={`删除别名 ${alias.email}`}
        title="删除别名"
        disabled={deleteAlias.isPending}
        onClick={() => {
          deleteAlias.reset();
          setOpen(true);
        }}
      >
        {deleteAlias.isPending ? (
          <RefreshCw className="button-spinner" size={15} aria-hidden="true" />
        ) : (
          <Trash2 size={16} aria-hidden="true" />
        )}
      </button>
      <ConfirmDialog
        confirmLabel="删除别名"
        description={`删除“${alias.email}”后，此 Hide My Email 别名将从账户中移除，且无法恢复。`}
        destructive
        onConfirm={() => deleteAlias.mutateAsync().then(() => undefined)}
        onConfirmError={(error) => {
          notify({ message: getApiErrorMessage(error), title: "删除别名失败", tone: "error" });
        }}
        onOpenChange={handleOpenChange}
        open={open}
        pending={deleteAlias.isPending}
        returnFocusRef={deleteButtonRef}
        title="确认删除别名？"
      />
    </>
  );
}
