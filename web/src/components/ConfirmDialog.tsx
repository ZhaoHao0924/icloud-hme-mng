import * as AlertDialog from "@radix-ui/react-alert-dialog";
import { LoaderCircle } from "lucide-react";
import { useState, type MouseEvent, type RefObject } from "react";

type ConfirmDialogProps = {
  cancelLabel?: string;
  confirmLabel?: string;
  description: string;
  destructive?: boolean;
  onConfirm: () => void | Promise<void>;
  onConfirmError?: (error: unknown) => void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  pending?: boolean;
  returnFocusRef?: RefObject<HTMLButtonElement | null>;
  title: string;
};

export function ConfirmDialog({
  cancelLabel = "取消",
  confirmLabel = "确认",
  description,
  destructive = false,
  onConfirm,
  onConfirmError,
  onOpenChange,
  open,
  pending = false,
  returnFocusRef,
  title,
}: ConfirmDialogProps) {
  const [submitting, setSubmitting] = useState(false);
  const busy = pending || submitting;

  async function handleConfirm(event: MouseEvent<HTMLButtonElement>) {
    event.preventDefault();
    if (busy) return;

    setSubmitting(true);
    try {
      await onConfirm();
      setSubmitting(false);
      onOpenChange(false);
    } catch (error) {
      setSubmitting(false);
      onConfirmError?.(error);
    }
  }

  return (
    <AlertDialog.Root open={open} onOpenChange={(nextOpen) => !busy && onOpenChange(nextOpen)}>
      <AlertDialog.Portal>
        <AlertDialog.Overlay className="dialog-overlay" />
        <AlertDialog.Content
          className="dialog-content"
          onCloseAutoFocus={(event) => {
            const target = returnFocusRef?.current;
            if (target?.isConnected) {
              event.preventDefault();
              target.focus();
            }
          }}
        >
          <AlertDialog.Title className="dialog-title">{title}</AlertDialog.Title>
          <AlertDialog.Description className="dialog-description">
            {description}
          </AlertDialog.Description>
          <div className="dialog-actions">
            <AlertDialog.Cancel asChild>
              <button className="button button-secondary" type="button" disabled={busy}>
                {cancelLabel}
              </button>
            </AlertDialog.Cancel>
            <AlertDialog.Action asChild>
              <button
                className={`button ${destructive ? "button-danger" : "button-primary"}`}
                type="button"
                disabled={busy}
                onClick={handleConfirm}
              >
                {busy ? (
                  <LoaderCircle className="button-spinner" size={16} aria-hidden="true" />
                ) : null}
                {busy ? "处理中" : confirmLabel}
              </button>
            </AlertDialog.Action>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
