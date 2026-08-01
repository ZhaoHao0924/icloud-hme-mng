import type { ReactNode } from "react";

type EmptyStateProps = {
  action?: ReactNode;
  description?: string;
  icon?: ReactNode;
  title: string;
};

export function EmptyState({ action, description, icon, title }: EmptyStateProps) {
  return (
    <div className="empty-state" role="status">
      <div className="empty-state-content">
        {icon ? (
          <span className="empty-state-icon" aria-hidden="true">
            {icon}
          </span>
        ) : null}
        <h3>{title}</h3>
        {description ? <p>{description}</p> : null}
        {action ? <div className="empty-state-action">{action}</div> : null}
      </div>
    </div>
  );
}
