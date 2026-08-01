import { AlertTriangle } from "lucide-react";
import type { ReactNode } from "react";

type ErrorStateProps = {
  action?: ReactNode;
  description: string;
  title?: string;
};

export function ErrorState({ action, description, title = "加载失败" }: ErrorStateProps) {
  return (
    <div className="error-state" role="alert">
      <AlertTriangle size={20} aria-hidden="true" />
      <div className="error-state-content">
        <h3>{title}</h3>
        <p>{description}</p>
        {action ? <div className="error-state-action">{action}</div> : null}
      </div>
    </div>
  );
}
