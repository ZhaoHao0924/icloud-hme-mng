import { PauseCircle, PlayCircle, RefreshCw } from "lucide-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { api, getApiErrorMessage } from "../../api/client";
import { queryKeys } from "../../api/queries";
import type { Alias } from "../../api/schemas";
import { useNotifications } from "../../components/notificationContext";

type AliasStatusButtonProps = {
  accountId: string;
  alias: Alias;
};

export function AliasStatusButton({ accountId, alias }: AliasStatusButtonProps) {
  const queryClient = useQueryClient();
  const { notify } = useNotifications();
  const nextActive = !alias.active;
  const actionLabel = nextActive ? "恢复" : "停用";
  const mutation = useMutation({
    mutationFn: () =>
      nextActive
        ? api.reactivateAlias(accountId, alias.anonymousId)
        : api.deactivateAlias(accountId, alias.anonymousId),
    onError: (error) => {
      notify({
        message: getApiErrorMessage(error),
        title: `别名${actionLabel}失败`,
        tone: "error",
      });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.aliases(accountId) });
      notify({
        message: alias.email,
        title: `别名已${actionLabel}`,
        tone: "success",
      });
    },
    retry: false,
  });

  return (
    <button
      className="icon-button alias-status-button"
      type="button"
      aria-label={`${actionLabel}别名 ${alias.email}`}
      title={`${actionLabel}别名`}
      disabled={mutation.isPending}
      onClick={() => mutation.mutate()}
    >
      {mutation.isPending ? (
        <RefreshCw className="button-spinner" size={15} aria-hidden="true" />
      ) : nextActive ? (
        <PlayCircle size={16} aria-hidden="true" />
      ) : (
        <PauseCircle size={16} aria-hidden="true" />
      )}
    </button>
  );
}
