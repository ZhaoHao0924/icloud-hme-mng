import { X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import {
  NotificationContext,
  type Notification,
  type NotificationInput,
  type NotificationTone,
} from "./notificationContext";
let nextNotificationId = 1;

function toneLabel(tone: NotificationTone) {
  if (tone === "error") return "错误";
  if (tone === "success") return "成功";
  if (tone === "warning") return "警告";
  return "提示";
}

type NotificationProviderProps = {
  children: ReactNode;
};

export function NotificationProvider({ children }: NotificationProviderProps) {
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const timers = useRef(new Map<number, ReturnType<typeof setTimeout>>());

  const dismiss = useCallback((id: number) => {
    setNotifications((current) => current.filter((notification) => notification.id !== id));
    const timer = timers.current.get(id);
    if (timer) {
      clearTimeout(timer);
      timers.current.delete(id);
    }
  }, []);

  const notify = useCallback(
    ({ duration = 4000, message, title, tone = "info" }: NotificationInput) => {
      const id = nextNotificationId++;
      setNotifications((current) => [...current, { id, message, title, tone }]);
      if (duration > 0) {
        const timer = setTimeout(() => dismiss(id), duration);
        timers.current.set(id, timer);
      }
      return id;
    },
    [dismiss],
  );

  useEffect(() => {
    const activeTimers = timers.current;
    return () => {
      for (const timer of activeTimers.values()) {
        clearTimeout(timer);
      }
      activeTimers.clear();
    };
  }, []);

  const value = useMemo(() => ({ dismiss, notify }), [dismiss, notify]);

  return (
    <NotificationContext.Provider value={value}>
      {children}
      <div className="notification-region" aria-label="通知" aria-live="polite">
        {notifications.map((notification) => (
          <div
            className={`notification notification-${notification.tone}`}
            key={notification.id}
            role={notification.tone === "error" ? "alert" : "status"}
          >
            <div className="notification-copy">
              <span className="notification-tone">{toneLabel(notification.tone)}</span>
              <strong>{notification.title}</strong>
              {notification.message ? <p>{notification.message}</p> : null}
            </div>
            <button
              className="icon-button notification-dismiss"
              type="button"
              aria-label="关闭通知"
              title="关闭通知"
              onClick={() => dismiss(notification.id)}
            >
              <X size={16} aria-hidden="true" />
            </button>
          </div>
        ))}
      </div>
    </NotificationContext.Provider>
  );
}
