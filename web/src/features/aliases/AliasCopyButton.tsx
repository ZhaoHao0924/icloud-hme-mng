import { Check, Copy, LoaderCircle } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { useNotifications } from "../../components/notificationContext";

const copiedStateDuration = 2000;

type CopyState = "copied" | "copying" | "idle";

export function AliasCopyButton({ email }: { email: string }) {
  const [copyState, setCopyState] = useState<CopyState>("idle");
  const resetTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const { notify } = useNotifications();

  useEffect(
    () => () => {
      if (resetTimer.current) clearTimeout(resetTimer.current);
    },
    [],
  );

  async function copyEmail() {
    if (copyState === "copying") return;
    if (resetTimer.current) {
      clearTimeout(resetTimer.current);
      resetTimer.current = null;
    }
    setCopyState("copying");

    try {
      if (!navigator.clipboard || typeof navigator.clipboard.writeText !== "function") {
        throw new Error("Clipboard API unavailable");
      }
      await navigator.clipboard.writeText(email);
      setCopyState("copied");
      notify({ duration: 2400, message: email, title: "邮箱已复制", tone: "success" });
      resetTimer.current = setTimeout(() => {
        setCopyState("idle");
        resetTimer.current = null;
      }, copiedStateDuration);
    } catch {
      setCopyState("idle");
      notify({
        message: "无法写入剪贴板，请检查浏览器权限。",
        title: "复制失败",
        tone: "error",
      });
    }
  }

  const accessibleLabel =
    copyState === "copied"
      ? `已复制邮箱 ${email}`
      : copyState === "copying"
        ? `正在复制邮箱 ${email}`
        : `复制邮箱 ${email}`;

  return (
    <button
      className={`icon-button alias-copy-button${copyState === "copied" ? " alias-copy-button-copied" : ""}`}
      type="button"
      aria-label={accessibleLabel}
      title={copyState === "copied" ? "已复制" : "复制邮箱"}
      disabled={copyState === "copying"}
      onClick={() => void copyEmail()}
    >
      {copyState === "copied" ? (
        <Check size={15} aria-hidden="true" />
      ) : copyState === "copying" ? (
        <LoaderCircle className="button-spinner" size={15} aria-hidden="true" />
      ) : (
        <Copy size={15} aria-hidden="true" />
      )}
    </button>
  );
}
