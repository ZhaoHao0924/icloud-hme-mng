import { createContext, useContext } from "react";

export type NotificationTone = "error" | "info" | "success" | "warning";

export type NotificationInput = {
  duration?: number;
  message?: string;
  title: string;
  tone?: NotificationTone;
};

export type Notification = Required<Pick<NotificationInput, "title" | "tone">> &
  Pick<NotificationInput, "message"> & { id: number };

export type NotificationContextValue = {
  dismiss: (id: number) => void;
  notify: (input: NotificationInput) => number;
};

export const NotificationContext = createContext<NotificationContextValue | null>(null);

export function useNotifications() {
  const context = useContext(NotificationContext);
  if (!context) {
    throw new Error("useNotifications must be used within NotificationProvider");
  }
  return context;
}
